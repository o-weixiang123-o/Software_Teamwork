#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

import pytest


@pytest.mark.p1
def test_system_ping(rest_client):
    res = rest_client.get("/system/ping")
    assert res.status_code == 200
    assert res.text == "pong"


@pytest.mark.p1
def test_system_version(rest_client):
    res = rest_client.get("/system/version")
    assert res.status_code == 200
    payload = res.json()
    assert payload["code"] == 0, payload
    assert payload["data"], payload


@pytest.mark.p2
def test_system_status_requires_service_token(rest_client_noauth):
    res = rest_client_noauth.get("/system/status")
    assert res.status_code == 401
    payload = res.json()
    assert payload["code"] == 401, payload
    assert "X-Service-Token" in payload["message"], payload


@pytest.mark.p2
def test_system_log_config_routes_auth_and_validation(rest_client, rest_client_noauth):
    unauth = rest_client_noauth.get("/system/config/log")
    assert unauth.status_code == 401
    assert unauth.json()["code"] == 401
    res = rest_client.get("/system/status")
    assert res.status_code == 200
    payload = res.json()
    assert payload["code"] == 0, payload
    for key in ("doc_engine", "storage", "database", "redis"):
        assert key in payload["data"], payload


@pytest.mark.p2
def test_system_healthz_contract(rest_client_noauth):
    res = rest_client_noauth.get("/system/healthz")
    assert res.status_code in (200, 500)
    payload = res.json()
    assert isinstance(payload, dict), payload
    assert payload, payload


@pytest.mark.p2
def test_system_status_contract(rest_client):

    levels = rest_client.get("/system/config/log")
    assert levels.status_code == 200
    levels_payload = levels.json()
    assert levels_payload["code"] == 0, levels_payload
    assert isinstance(levels_payload["data"], dict), levels_payload

    missing_body = rest_client.put("/system/config/log", json={})
    assert missing_body.status_code == 200
    missing_payload = missing_body.json()
    assert missing_payload["code"] == 102, missing_payload
    assert "pkg_name and level are required" in missing_payload["message"], missing_payload

    invalid_level = rest_client.put("/system/config/log", json={"pkg_name": "rag", "level": "NOT_A_LEVEL"})
    assert invalid_level.status_code == 200
    invalid_payload = invalid_level.json()
    assert invalid_payload["code"] == 102, invalid_payload
    assert "Invalid log level" in invalid_payload["message"], invalid_payload
