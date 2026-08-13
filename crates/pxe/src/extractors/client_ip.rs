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

use std::net::IpAddr;

use axum::extract::FromRequestParts;
use axum::http::request::Parts;
use axum_client_ip::ClientIp;
use ipnet::IpNet;

use crate::common::AppState;
use crate::rpc_error::PxeRequestError;

pub(super) async fn extract(
    parts: &mut Parts,
    state: &AppState,
) -> Result<IpAddr, PxeRequestError> {
    let direct_ip = ClientIp::from_request_parts(parts, state)
        .await
        .map_err(PxeRequestError::MissingIp)?
        .0;

    if !is_trusted(direct_ip, &state.runtime_config.trusted_proxy_cidrs) {
        return Ok(direct_ip);
    }

    let Some(forwarded_for) = parts.headers.get("x-forwarded-for") else {
        return Ok(direct_ip);
    };
    select_forwarded_ip(
        direct_ip,
        forwarded_for
            .to_str()
            .map_err(|error| PxeRequestError::InvalidProxyHeader(error.to_string()))?,
        &state.runtime_config.trusted_proxy_cidrs,
    )
}

fn select_forwarded_ip(
    direct_ip: IpAddr,
    forwarded_for: &str,
    trusted_proxies: &[IpNet],
) -> Result<IpAddr, PxeRequestError> {
    let addresses = forwarded_for
        .split(',')
        .map(str::trim)
        .map(|value| {
            value.parse::<IpAddr>().map_err(|error| {
                PxeRequestError::InvalidProxyHeader(format!(
                    "X-Forwarded-For contains {value:?}: {error}"
                ))
            })
        })
        .collect::<Result<Vec<_>, _>>()?;

    addresses
        .into_iter()
        .rev()
        .find(|address| !is_trusted(*address, trusted_proxies))
        .ok_or_else(|| {
            PxeRequestError::InvalidProxyHeader(format!(
                "X-Forwarded-For has no client address before trusted proxy {direct_ip}"
            ))
        })
}

fn is_trusted(address: IpAddr, trusted_proxies: &[IpNet]) -> bool {
    trusted_proxies
        .iter()
        .any(|network| network.contains(&address))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn selects_rightmost_untrusted_forwarded_address() {
        let trusted = vec![
            "127.0.0.0/8".parse().unwrap(),
            "10.0.0.0/8".parse().unwrap(),
        ];
        assert_eq!(
            select_forwarded_ip(
                "127.0.0.1".parse().unwrap(),
                "192.0.2.15, 10.1.2.3",
                &trusted,
            )
            .unwrap(),
            "192.0.2.15".parse::<IpAddr>().unwrap(),
        );
    }

    #[test]
    fn rejects_malformed_forwarded_address() {
        let trusted = vec!["127.0.0.0/8".parse().unwrap()];
        assert!(select_forwarded_ip("127.0.0.1".parse().unwrap(), "not-an-ip", &trusted,).is_err());
    }
}
