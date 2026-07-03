#
#  Copyright 2024 The InfiniFlow Authors. All Rights Reserved.
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
import logging
import os
import sys
import time
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from quart import Blueprint, Quart, request, g, current_app, jsonify
from quart_cors import cors
from common.constants import RetCode
from api.db.db_models import close_connection
from api.utils.json_encode import CustomJSONEncoder

from quart_auth import Unauthorized as QuartAuthUnauthorized
from werkzeug.exceptions import Unauthorized as WerkzeugUnauthorized
from quart_schema import QuartSchema
from common import settings
from api.utils.api_utils import server_error_response, get_json_result, build_error_result
from api.constants import API_VERSION
from common.exceptions import ModelException
from api.route_registry import collect_runtime_page_paths
from api.utils.gateway_auth import SERVICE_TOKEN_HEADER, normalize_route_auth_types, route_allows_gateway_auth, service_token_is_valid
from api.utils.runtime_scope import runtime_subject

settings.init_settings()

__all__ = ["app"]

UNAUTHORIZED_MESSAGE = "<Unauthorized '401: Unauthorized'>"


def _unauthorized_message(error):
    if error is None:
        return UNAUTHORIZED_MESSAGE

    description = getattr(error, "description", None)
    if description:
        return description

    try:
        return repr(error)
    except Exception:
        return UNAUTHORIZED_MESSAGE


app = Quart(__name__)
app = cors(app, allow_origin="*")

# openapi supported
QuartSchema(app)

app.url_map.strict_slashes = False
app.json_encoder = CustomJSONEncoder
app.errorhandler(Exception)(server_error_response)

# Configure Quart timeouts for slow LLM responses (e.g., local Ollama on CPU)
# Default Quart timeouts are 60 seconds which is too short for many LLM backends
app.config["RESPONSE_TIMEOUT"] = int(os.environ.get("QUART_RESPONSE_TIMEOUT", 600))
app.config["BODY_TIMEOUT"] = int(os.environ.get("QUART_BODY_TIMEOUT", 600))

## convince for dev and debug
app.config["MAX_CONTENT_LENGTH"] = int(
    os.environ.get("MAX_CONTENT_LENGTH", 1024 * 1024 * 1024)
)
app.config['SECRET_KEY'] = settings.get_secret_key()
app.secret_key = settings.get_secret_key()

from functools import wraps
from typing import ParamSpec, TypeVar
from collections.abc import Awaitable, Callable
from werkzeug.local import LocalProxy

T = TypeVar("T")
P = ParamSpec("P")

AUTH_GATEWAY = "GATEWAY"
DEFAULT_AUTH_TYPES = (AUTH_GATEWAY,)


def _normalize_auth_types(auth_types=None):
    return normalize_route_auth_types(auth_types, DEFAULT_AUTH_TYPES)


def _load_user(auth_types=None):
    explicit_auth_types = auth_types is not None
    auth_types = _normalize_auth_types(auth_types)
    if not route_allows_gateway_auth(auth_types, DEFAULT_AUTH_TYPES):
        g.auth_error_message = "Gateway auth is required for runtime routes"
        return None
    if getattr(g, "user", None) and (not explicit_auth_types or getattr(g, "auth_type", None) in auth_types):
        return g.user

    g.user = None
    g.auth_type = None
    g.auth_error_message = None

    if not service_token_is_valid(request.headers):
        g.auth_error_message = f"Invalid or missing {SERVICE_TOKEN_HEADER}"
        return None

    user = runtime_subject()
    g.auth_type = AUTH_GATEWAY
    g.user = user
    return user


current_user = LocalProxy(_load_user)


def login_required(func: Callable[P, Awaitable[T]] = None, auth_types=None) -> Callable[P, Awaitable[T]]:
    """Require the global runtime auth subject before executing a route."""

    def decorator(func: Callable[P, Awaitable[T]]) -> Callable[P, Awaitable[T]]:
        @wraps(func)
        async def wrapper(*args: P.args, **kwargs: P.kwargs) -> T:
            timing_enabled = os.getenv("RAGFLOW_API_TIMING")
            t_start = time.perf_counter() if timing_enabled else None
            user = _load_user(auth_types)
            if timing_enabled:
                logging.info(
                    "api_timing login_required auth_ms=%.2f path=%s",
                    (time.perf_counter() - t_start) * 1000,
                    request.path,
                )
            if not user:
                message = getattr(g, "auth_error_message", None) or f"Invalid or missing {SERVICE_TOKEN_HEADER}"
                return build_error_result(code=RetCode.UNAUTHORIZED, message=message)
            return await current_app.ensure_async(func)(*args, **kwargs)

        return wrapper

    if func is None:
        return decorator
    return decorator(func)


def search_pages_path(page_path):
    return collect_runtime_page_paths(page_path)


def register_page(page_path):
    path = f"{page_path}"

    page_name = page_path.stem.removesuffix("_app")
    module_name = ".".join(page_path.parts[page_path.parts.index("api") : -1] + (page_name,))

    spec = spec_from_file_location(module_name, page_path)
    page = module_from_spec(spec)
    page.app = app
    page.manager = Blueprint(page_name, module_name)
    sys.modules[module_name] = page
    spec.loader.exec_module(page)
    page_name = getattr(page, "page_name", page_name)
    restful_api_path = "\\restful_apis\\" if sys.platform.startswith("win") else "/restful_apis/"
    url_prefix = f"/api/{API_VERSION}" if restful_api_path in path else f"/{API_VERSION}/{page_name}"

    app.register_blueprint(page.manager, url_prefix=url_prefix)
    return url_prefix


pages_dir = [
    Path(__file__).parent,
    Path(__file__).parent.parent / "api" / "apps",
    Path(__file__).parent.parent / "api" / "apps" / "restful_apis",
]

client_urls_prefix = [register_page(path) for directory in pages_dir for path in search_pages_path(directory)]


@app.errorhandler(404)
async def not_found(error):
    logging.error(f"The requested URL {request.path} was not found")
    message = f"Not Found: {request.path}"
    response = {
        "code": RetCode.NOT_FOUND,
        "message": message,
        "data": None,
        "error": "Not Found",
    }
    return jsonify(response), RetCode.NOT_FOUND


@app.errorhandler(401)
async def unauthorized(error):
    logging.warning("Unauthorized request")
    return get_json_result(code=RetCode.UNAUTHORIZED, message=_unauthorized_message(error)), RetCode.UNAUTHORIZED


@app.errorhandler(QuartAuthUnauthorized)
async def unauthorized_quart_auth(error):
    logging.warning("Unauthorized request (quart_auth)")
    return get_json_result(code=RetCode.UNAUTHORIZED, message=repr(error)), RetCode.UNAUTHORIZED


@app.errorhandler(WerkzeugUnauthorized)
async def unauthorized_werkzeug(error):
    logging.warning("Unauthorized request (werkzeug)")
    return get_json_result(code=error.code, message=error.description), RetCode.UNAUTHORIZED


@app.errorhandler(ModelException)
async def handle_model_exception(error):
    logging.warning("Forbidden request")
    return get_json_result(code=RetCode.BAD_REQUEST, message=repr(error)), 200


@app.teardown_request
def _db_close(exception):
    if exception:
        logging.exception(f"Request failed: {exception}")
    close_connection()
