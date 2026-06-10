/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

//! Periodic republishing of current `ManagedHostState`.
//!
//! [`MqttStateChangeHook`](super::hook::MqttStateChangeHook) publishes state on
//! every transition. Integrators that cannot poll the NICo API (e.g. they are
//! network-restricted) can miss a transition and never reconcile. This module
//! walks the current managed hosts on a timer and re-publishes their state to
//! the same `{topic_prefix}/{machineId}/state` topic with the same JSON payload
//! as change-driven events, so downstream consumers handle both identically and
//! can self-heal.
//!
//! Tuning is driven by [`PeriodicStateRepublishConfig`]: sweep `interval`,
//! whether to publish all hosts or only unhealthy ones (`scope`), how often
//! healthy hosts are re-published relative to unhealthy ones
//! (`healthy_republish_every`), and an optional per-sweep publish rate limit
//! (`max_publishes_per_second`).

use std::time::Duration;

use carbide_mqtt_common::hook::MqttPublisher;
use carbide_mqtt_common::metrics::MqttHookMetrics;
use carbide_uuid::machine::MachineId;
use chrono::{DateTime, Utc};
use health_report::HealthReport;
use model::machine::{HostHealthConfig, LoadSnapshotOptions, ManagedHostState};
use opentelemetry::metrics::Meter;
use tokio::task::JoinSet;
use tokio::time::{Instant, timeout_at};
use tokio_util::sync::CancellationToken;

use crate::cfg::file::{PeriodicStateRepublishConfig, RepublishScope};
use crate::mqtt_state_change_hook::message::ManagedHostStateChangeMessage;

/// Periodically re-publishes current `ManagedHostState` for managed hosts to
/// the DSX Exchange Event Bus.
///
/// Unlike [`MqttStateChangeHook`](super::hook::MqttStateChangeHook), which
/// buffers change events in a bounded queue, the republisher publishes directly
/// from its sweep. A full sweep of a large site can be many publishes, so it
/// would otherwise risk overflowing (and dropping) the change-event queue;
/// publishing directly keeps the two paths independent and lets
/// `max_publishes_per_second` bound the burst.
pub struct ManagedHostStateRepublisher<P: MqttPublisher> {
    publisher: P,
    db_pool: sqlx::PgPool,
    topic_prefix: String,
    publish_timeout: Duration,
    config: PeriodicStateRepublishConfig,
    host_health_config: HostHealthConfig,
    metrics: MqttHookMetrics,
}

/// Constructor parameters for [`ManagedHostStateRepublisher`].
pub struct ManagedHostStateRepublisherParams {
    pub db_pool: sqlx::PgPool,
    pub topic_prefix: String,
    pub publish_timeout: Duration,
    pub config: PeriodicStateRepublishConfig,
    pub host_health_config: HostHealthConfig,
}

impl<P: MqttPublisher> ManagedHostStateRepublisher<P> {
    /// Create a new republisher.
    ///
    /// Reuses the change-hook publish metrics (`carbide_dsx_event_bus_publish_count`)
    /// under the `managed_host_republish` component so periodic publishes can be
    /// told apart from change-driven ones on dashboards.
    pub fn new(publisher: P, params: ManagedHostStateRepublisherParams, meter: &Meter) -> Self {
        let metrics = MqttHookMetrics::without_queue_depth(meter, "managed_host_republish");
        Self {
            publisher,
            db_pool: params.db_pool,
            topic_prefix: params.topic_prefix,
            publish_timeout: params.publish_timeout,
            config: params.config,
            host_health_config: params.host_health_config,
            metrics,
        }
    }

    /// Spawn the republisher's background sweep loop into `join_set` when
    /// enabled. A no-op when `config.enabled` is false.
    pub fn start(
        self,
        join_set: &mut JoinSet<()>,
        cancel_token: CancellationToken,
    ) -> std::io::Result<()> {
        if self.config.enabled {
            tracing::info!(
                interval_secs = self.config.interval.as_secs(),
                scope = ?self.config.scope,
                healthy_republish_every = self.config.healthy_republish_every,
                max_publishes_per_second = self.config.max_publishes_per_second,
                "Starting periodic managed host state republisher"
            );
            join_set
                .build_task()
                .name("managed_host_state_republisher")
                .spawn(async move { self.run(cancel_token).await })?;
        }

        Ok(())
    }

    async fn run(self, cancel_token: CancellationToken) {
        let mut sweep: u64 = 0;
        loop {
            let publish_healthy = should_publish_healthy(
                self.config.scope,
                self.config.healthy_republish_every,
                sweep,
            );
            if let Err(e) = self.run_sweep(publish_healthy, &cancel_token).await {
                tracing::warn!(error = %e, "Managed host state republish sweep failed");
            }
            sweep = sweep.wrapping_add(1);

            tokio::select! {
                _ = tokio::time::sleep(self.config.interval) => {}
                _ = cancel_token.cancelled() => {
                    tracing::debug!("Managed host state republisher stop requested");
                    return;
                }
            }
        }
    }

    /// Load every managed host and re-publish those selected by the current
    /// scope. Unhealthy hosts are always published; healthy hosts are published
    /// only when `publish_healthy` is true for this sweep.
    async fn run_sweep(
        &self,
        publish_healthy: bool,
        cancel_token: &CancellationToken,
    ) -> eyre::Result<()> {
        // Instance data is not needed: the message only carries `machine_id`
        // and `managed_state` (read from the host's own state column), and
        // aggregate health is derived from host + DPU snapshots.
        let options = LoadSnapshotOptions {
            include_history: false,
            include_instance_data: false,
            host_health_config: self.host_health_config,
        };
        let snapshots = db::managed_host::load_all(&self.db_pool, options).await?;

        let pacing = pacing_delay(self.config.max_publishes_per_second);
        // One timestamp for the whole sweep: it marks when NICo asserts this
        // state, not when each individual publish happened.
        let timestamp = Utc::now();
        let total = snapshots.len();
        let mut published = 0usize;
        let mut skipped_healthy = 0usize;

        for snapshot in &snapshots {
            if cancel_token.is_cancelled() {
                break;
            }

            let unhealthy = report_is_unhealthy(&snapshot.aggregate_health);
            if !should_publish(unhealthy, publish_healthy) {
                skipped_healthy += 1;
                continue;
            }

            publish_state(
                &self.publisher,
                &self.topic_prefix,
                self.publish_timeout,
                &self.metrics,
                &snapshot.host_snapshot.id,
                &snapshot.managed_state,
                timestamp,
            )
            .await;
            published += 1;

            if let Some(delay) = pacing {
                tokio::select! {
                    _ = tokio::time::sleep(delay) => {}
                    _ = cancel_token.cancelled() => break,
                }
            }
        }

        tracing::info!(
            total,
            published,
            skipped_healthy,
            publish_healthy,
            scope = ?self.config.scope,
            "Managed host state republish sweep complete"
        );

        Ok(())
    }
}

/// Whether healthy hosts should be published on a given sweep (0-indexed).
///
/// `UnhealthyOnly` never publishes healthy hosts. `All` publishes them every
/// `healthy_republish_every` sweeps (sweep 0 always publishes); `0` is treated
/// as `1`.
fn should_publish_healthy(scope: RepublishScope, healthy_republish_every: u32, sweep: u64) -> bool {
    match scope {
        RepublishScope::UnhealthyOnly => false,
        RepublishScope::All => {
            let every = u64::from(healthy_republish_every.max(1));
            sweep.is_multiple_of(every)
        }
    }
}

/// Whether a host should be published this sweep: unhealthy hosts always are;
/// healthy hosts only when `publish_healthy` is set for the sweep.
fn should_publish(unhealthy: bool, publish_healthy: bool) -> bool {
    unhealthy || publish_healthy
}

/// A managed host is "unhealthy" when its aggregate health carries at least one
/// alert.
fn report_is_unhealthy(report: &HealthReport) -> bool {
    !report.alerts.is_empty()
}

/// Per-publish delay that bounds a sweep to `max_publishes_per_second`, or
/// `None` when unbounded (`0`).
fn pacing_delay(max_publishes_per_second: u32) -> Option<Duration> {
    (max_publishes_per_second > 0)
        .then(|| Duration::from_secs_f64(1.0 / f64::from(max_publishes_per_second)))
}

/// Serialize and publish a single managed host's current state, bounded by
/// `publish_timeout`, recording the outcome in `metrics`.
async fn publish_state<P: MqttPublisher>(
    publisher: &P,
    topic_prefix: &str,
    publish_timeout: Duration,
    metrics: &MqttHookMetrics,
    machine_id: &MachineId,
    state: &ManagedHostState,
    timestamp: DateTime<Utc>,
) {
    let message = ManagedHostStateChangeMessage {
        machine_id,
        managed_host_state: state,
        timestamp,
    };

    let payload = match message.to_json_bytes() {
        Ok(payload) => payload,
        Err(e) => {
            tracing::error!(
                machine_id = %machine_id,
                error = %e,
                "Failed to serialize managed host state for republish"
            );
            metrics.record_serialization_error();
            return;
        }
    };

    let topic = format!("{topic_prefix}/{machine_id}/state");
    let deadline = Instant::now() + publish_timeout;
    match timeout_at(deadline, publisher.publish(&topic, payload)).await {
        Ok(Ok(())) => {
            tracing::debug!(topic = %topic, "Republished managed host state to MQTT");
            metrics.record_success();
        }
        Ok(Err(e)) => {
            tracing::warn!(topic = %topic, error = %e, "Failed to republish managed host state");
            metrics.record_publish_error();
        }
        Err(_) => {
            tracing::warn!(topic = %topic, "Managed host state republish timed out");
            metrics.record_timeout();
        }
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;
    use std::sync::atomic::{AtomicUsize, Ordering};

    use carbide_uuid::machine::{MachineIdSource, MachineType};
    use mqttea::MqtteaClientError;
    use opentelemetry::global;
    use tokio::sync::Barrier;

    use super::*;

    fn test_meter() -> Meter {
        global::meter("republisher-test")
    }

    fn test_metrics() -> MqttHookMetrics {
        MqttHookMetrics::without_queue_depth(&test_meter(), "managed_host_republish_test")
    }

    fn test_machine_id() -> MachineId {
        MachineId::new(
            MachineIdSource::ProductBoardChassisSerial,
            [0; 32],
            MachineType::Host,
        )
    }

    /// Publisher that forwards each published (topic, payload) over a channel.
    struct SignalingPublisher {
        sender: tokio::sync::mpsc::UnboundedSender<(String, Vec<u8>)>,
    }

    impl SignalingPublisher {
        fn new() -> (
            Self,
            tokio::sync::mpsc::UnboundedReceiver<(String, Vec<u8>)>,
        ) {
            let (sender, receiver) = tokio::sync::mpsc::unbounded_channel();
            (Self { sender }, receiver)
        }
    }

    #[async_trait::async_trait]
    impl MqttPublisher for SignalingPublisher {
        async fn publish(&self, topic: &str, payload: Vec<u8>) -> Result<(), MqtteaClientError> {
            let _ = self.sender.send((topic.to_string(), payload));
            Ok(())
        }
    }

    #[test]
    fn unhealthy_only_never_publishes_healthy() {
        for sweep in 0..5 {
            assert!(!should_publish_healthy(
                RepublishScope::UnhealthyOnly,
                1,
                sweep
            ));
        }
    }

    #[test]
    fn all_scope_every_one_publishes_healthy_every_sweep() {
        for sweep in 0..5 {
            assert!(should_publish_healthy(RepublishScope::All, 1, sweep));
        }
    }

    #[test]
    fn all_scope_zero_cadence_is_treated_as_one() {
        for sweep in 0..5 {
            assert!(should_publish_healthy(RepublishScope::All, 0, sweep));
        }
    }

    #[test]
    fn all_scope_every_three_publishes_healthy_on_multiples() {
        let got: Vec<bool> = (0..7)
            .map(|sweep| should_publish_healthy(RepublishScope::All, 3, sweep))
            .collect();
        assert_eq!(
            got,
            vec![true, false, false, true, false, false, true],
            "healthy hosts publish on sweeps 0, 3, 6"
        );
    }

    #[test]
    fn unhealthy_hosts_always_publish_regardless_of_healthy_flag() {
        assert!(should_publish(true, false));
        assert!(should_publish(true, true));
    }

    #[test]
    fn healthy_hosts_publish_only_when_flag_set() {
        assert!(!should_publish(false, false));
        assert!(should_publish(false, true));
    }

    #[test]
    fn report_with_no_alerts_is_healthy() {
        assert!(!report_is_unhealthy(&HealthReport::empty(
            "test".to_string()
        )));
    }

    #[test]
    fn report_with_an_alert_is_unhealthy() {
        // `missing_report` carries a single alert.
        assert!(report_is_unhealthy(&HealthReport::missing_report()));
    }

    #[test]
    fn pacing_disabled_when_zero() {
        assert_eq!(pacing_delay(0), None);
    }

    #[test]
    fn pacing_divides_one_second_by_rate() {
        assert_eq!(pacing_delay(10), Some(Duration::from_millis(100)));
        assert_eq!(pacing_delay(1), Some(Duration::from_secs(1)));
    }

    #[tokio::test]
    async fn publish_state_uses_state_topic_and_payload() {
        let (publisher, mut receiver) = SignalingPublisher::new();
        let metrics = test_metrics();
        let id = test_machine_id();
        let state = ManagedHostState::Ready;

        publish_state(
            &publisher,
            "NICO/v1/machine",
            Duration::from_secs(1),
            &metrics,
            &id,
            &state,
            Utc::now(),
        )
        .await;

        let (topic, payload) = receiver.recv().await.expect("should receive message");
        assert_eq!(topic, format!("NICO/v1/machine/{id}/state"));

        let parsed: serde_json::Value = serde_json::from_slice(&payload).unwrap();
        assert_eq!(
            parsed
                .get("managed_host_state")
                .unwrap()
                .get("state")
                .unwrap(),
            "ready"
        );
        assert!(parsed.get("machine_id").is_some());
        assert!(parsed.get("timestamp").is_some());
    }

    #[tokio::test]
    async fn publish_state_respects_publish_timeout() {
        let started = Arc::new(Barrier::new(2));
        let call_count = Arc::new(AtomicUsize::new(0));
        let complete_count = Arc::new(AtomicUsize::new(0));

        struct TimeoutPublisher {
            started: Arc<Barrier>,
            call_count: Arc<AtomicUsize>,
            complete_count: Arc<AtomicUsize>,
        }

        #[async_trait::async_trait]
        impl MqttPublisher for TimeoutPublisher {
            async fn publish(&self, _: &str, _: Vec<u8>) -> Result<(), MqtteaClientError> {
                self.call_count.fetch_add(1, Ordering::SeqCst);
                self.started.wait().await;
                std::future::pending::<()>().await;
                self.complete_count.fetch_add(1, Ordering::SeqCst);
                Ok(())
            }
        }

        let publisher = TimeoutPublisher {
            started: started.clone(),
            call_count: call_count.clone(),
            complete_count: complete_count.clone(),
        };
        let metrics = test_metrics();
        let id = test_machine_id();
        let state = ManagedHostState::Ready;

        let publish = publish_state(
            &publisher,
            "NICO/v1/machine",
            Duration::from_millis(10),
            &metrics,
            &id,
            &state,
            Utc::now(),
        );

        // The publish returns once the timeout fires; the inner publish never
        // completes (it is parked on `pending`).
        tokio::join!(publish, async {
            started.wait().await;
        });

        assert_eq!(call_count.load(Ordering::SeqCst), 1);
        assert_eq!(complete_count.load(Ordering::SeqCst), 0);
    }
}
