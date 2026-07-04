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
import logging

from peewee import OperationalError
from quart import request
from common.constants import RetCode
from api.apps import login_required, current_user
from api.utils.api_utils import get_error_argument_result, get_error_data_result, get_json_result, get_result, add_scope_id_to_kwargs
from api.utils.pagination_utils import validate_rest_api_page_size
from api.utils.validation_utils import (
    CreateDatasetReq,
    DeleteDatasetReq,
    InternalCreateDatasetReq,
    ListDatasetReq,
    SearchDatasetReq,
    SearchDatasetsReq,
    UpdateDatasetReq,
    validate_and_parse_json_request,
    validate_and_parse_request_args,
)
from api.apps.services import dataset_api_service


async def _create_dataset_with_validator(scope_id: str | None, validator):
    req, err = await validate_and_parse_json_request(request, validator)
    if err is not None:
        return get_error_argument_result(err)

    try:
        if not scope_id:
            scope_id = current_user.id
        success, result = await dataset_api_service.create_dataset(scope_id, req)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except LookupError as e:
        return get_error_argument_result(str(e))
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


def _with_status(response, status_code):
    response.status_code = status_code
    return response


def _get_search_business_error_result(result):
    if isinstance(result, dataset_api_service.SearchBusinessError):
        return _with_status(get_result(code=result.code, message=result.message), result.http_status)
    return _with_status(get_error_data_result(message=result, code=RetCode.EXCEPTION_ERROR), 502)


def _get_search_argument_error_result(message):
    return _with_status(get_error_argument_result(message), 400)


def _get_search_dependency_error_result(message="Internal server error"):
    return _with_status(get_error_data_result(message=message, code=RetCode.EXCEPTION_ERROR), 502)


@manager.route("/datasets/tags/aggregation", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def aggregate_tags(scope_id):
    dataset_ids = request.args.get("dataset_ids", "").split(",")
    dataset_ids = [d for d in dataset_ids if d]
    if not dataset_ids:
        return get_error_data_result(message="Lack of dataset_ids in query parameters")

    try:
        success, result = dataset_api_service.aggregate_tags(dataset_ids, scope_id)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/metadata/flattened", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def get_flattened_metadata(scope_id):
    dataset_ids = request.args.get("dataset_ids", "").split(",")
    dataset_ids = [d for d in dataset_ids if d]
    if not dataset_ids:
        return get_error_data_result(message="Lack of dataset_ids in query parameters")

    try:
        success, result = dataset_api_service.get_flattened_metadata(dataset_ids, scope_id)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets", methods=["POST"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def create(scope_id: str = None):
    """
    Create a new dataset.
    ---
    tags:
      - Datasets
    security:
      - ApiKeyAuth: []
    parameters:
      - in: header
        name: Authorization
        type: string
        required: true
        description: Bearer token for authentication.
      - in: body
        name: body
        description: Dataset creation parameters.
        required: true
        schema:
          type: object
          required:
            - name
          properties:
            name:
              type: string
              description: Dataset name (required).
            avatar:
              type: string
              description: Optional base64-encoded avatar image.
            description:
              type: string
              description: Optional dataset description.
            embedding_model:
              type: string
              description: Optional embedding model name; if omitted, the scope's default embedding model is used.
            permission:
              type: string
              enum: ['me', 'team']
              description: Visibility of the dataset (private to me or shared with team).
            chunk_method:
              type: string
              enum: ["naive", "book", "email", "laws", "manual", "one", "paper",
                     "picture", "presentation", "qa", "table", "tag"]
              description: Chunking method; if omitted, defaults to "naive".
            parser_config:
              type: object
              description: Optional parser configuration; server-side defaults will be applied.
    responses:
      200:
        description: Successful operation.
        schema:
          type: object
          properties:
            data:
              type: object
    """
    return await _create_dataset_with_validator(scope_id, CreateDatasetReq)


@manager.route("/internal/datasets", methods=["POST"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def create_internal(scope_id: str = None):
    """Create a dataset from trusted internal services."""
    return await _create_dataset_with_validator(scope_id, InternalCreateDatasetReq)


@manager.route("/datasets", methods=["DELETE"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def delete(scope_id):
    """
    Delete datasets.
    ---
    tags:
      - Datasets
    security:
      - ApiKeyAuth: []
    parameters:
      - in: header
        name: Authorization
        type: string
        required: true
        description: Bearer token for authentication.
      - in: body
        name: body
        description: Dataset deletion parameters.
        required: true
        schema:
          type: object
          required:
            - ids
          properties:
            ids:
              type: array or null
              items:
                type: string
              description: |
                Specifies the datasets to delete:
                - If `null`, all datasets will be deleted.
                - If an array of IDs, only the specified datasets will be deleted.
                - If an empty array, no datasets will be deleted.
    responses:
      200:
        description: Successful operation.
        schema:
          type: object
    """
    req, err = await validate_and_parse_json_request(request, DeleteDatasetReq)
    if err is not None:
        return get_error_argument_result(err)

    try:
        success, result = await dataset_api_service.delete_datasets(scope_id, req.get("ids"), req.get("delete_all", False))
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except OperationalError as e:
        logging.exception(e)
        return get_error_data_result(message="Database operation failed")
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>", methods=["PUT"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def update(scope_id, dataset_id):
    """
    Update a dataset.
    ---
    tags:
      - Datasets
    security:
      - ApiKeyAuth: []
    parameters:
      - in: path
        name: dataset_id
        type: string
        required: true
        description: ID of the dataset to update.
      - in: header
        name: Authorization
        type: string
        required: true
        description: Bearer token for authentication.
      - in: body
        name: body
        description: Dataset update parameters.
        required: true
        schema:
          type: object
          properties:
            name:
              type: string
              description: New name of the dataset.
            avatar:
              type: string
              description: Updated base64 encoding of the avatar.
            description:
              type: string
              description: Updated description of the dataset.
            embedding_model:
              type: string
              description: Updated embedding model Name.
            permission:
              type: string
              enum: ['me', 'team']
              description: Updated dataset permission.
            chunk_method:
              type: string
              enum: ["naive", "book", "email", "laws", "manual", "one", "paper",
                     "picture", "presentation", "qa", "table", "tag"
                     ]
              description: Updated chunking method.
            pagerank:
              type: integer
              description: Updated page rank.
            parser_config:
              type: object
              description: Updated parser configuration.
    responses:
      200:
        description: Successful operation.
        schema:
          type: object
    """
    # Field name transformations during model dump:
    # | Original       | Dump Output  |
    # |----------------|-------------|
    # | embedding_model| embd_id     |
    # | chunk_method   | parser_id   |
    extras = {"dataset_id": dataset_id}
    req, err = await validate_and_parse_json_request(request, UpdateDatasetReq, extras=extras, exclude_unset=True)
    if err is not None:
        return get_error_argument_result(err)

    try:
        success, result = await dataset_api_service.update_dataset(scope_id, dataset_id, req)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except OperationalError as e:
        logging.exception(e)
        return get_error_data_result(message="Database operation failed")
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def list_datasets(scope_id):
    """
    List datasets.
    ---
    tags:
      - Datasets
    security:
      - ApiKeyAuth: []
    parameters:
      - in: query
        name: id
        type: string
        required: false
        description: Dataset ID to filter.
      - in: query
        name: name
        type: string
        required: false
        description: Dataset name to filter.
      - in: query
        name: page
        type: integer
        required: false
        default: 1
        description: Page number.
      - in: query
        name: page_size
        type: integer
        required: false
        default: 30
        description: Number of items per page.
      - in: query
        name: orderby
        type: string
        required: false
        default: "create_time"
        description: Field to order by.
      - in: query
        name: desc
        type: boolean
        required: false
        default: true
        description: Order in descending.
      - in: header
        name: Authorization
        type: string
        required: true
        description: Bearer token for authentication.
    responses:
      200:
        description: Successful operation.
        schema:
          type: array
          items:
            type: object
    """
    args, err = validate_and_parse_request_args(request, ListDatasetReq)
    if err is not None:
        return get_error_argument_result(err)

    try:
        success, result = dataset_api_service.list_datasets(scope_id, args)
        if success:
            return get_result(data=result.get("data"), total=result.get("total"))
        else:
            return get_error_data_result(message=result)
    except OperationalError as e:
        logging.exception(e)
        return get_error_data_result(message="Database operation failed")
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def get_dataset(scope_id, dataset_id):
    try:
        success, result = dataset_api_service.get_dataset(dataset_id, scope_id)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/ingestions/summary", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def get_ingestion_summary(scope_id, dataset_id):
    try:
        success, result = dataset_api_service.get_ingestion_summary(dataset_id, scope_id)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/tags", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def list_tags(scope_id, dataset_id):
    try:
        success, result = dataset_api_service.list_tags(dataset_id, scope_id)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/tags", methods=["DELETE"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def delete_tags(scope_id, dataset_id):
    req = await request.get_json()
    if not req or "tags" not in req:
        return get_error_data_result(message="Lack of tags in request body")
    if not isinstance(req["tags"], list) or not all(isinstance(t, str) for t in req["tags"]):
        return get_error_argument_result("tags must be a list of strings")

    try:
        success, result = dataset_api_service.delete_tags(dataset_id, scope_id, req["tags"])
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/tags", methods=["PUT"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def rename_tag(scope_id, dataset_id):
    req = await request.get_json()
    if not req or "from_tag" not in req or "to_tag" not in req:
        return get_error_data_result(message="Lack of from_tag or to_tag in request body")
    if not isinstance(req["from_tag"], str) or not isinstance(req["to_tag"], str):
        return get_error_argument_result("from_tag and to_tag must be strings")

    if not req["from_tag"].strip() or not req["to_tag"].strip():
        return get_error_argument_result("from_tag and to_tag must not be empty")

    try:
        success, result = dataset_api_service.rename_tag(dataset_id, scope_id, req["from_tag"], req["to_tag"])
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/search", methods=["POST"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def search_datasets(scope_id):
    """Search (retrieval test) across multiple datasets.

    POST /api/v1/datasets/search
    JSON body: {"dataset_ids": list[str] (required), "question": str (required), "doc_ids": list[str], "top_k": int, "page": int, "size": int,
               "similarity_threshold": float, "vector_similarity_weight": float, "use_kg": bool,
               "cross_languages": list[str], "keyword": bool, "meta_data_filter": dict}
    Success: {"code": 0, "data": {"chunks": [...], "total": int, "labels": [...]}}
    Errors: ARGUMENT_ERROR (101) with HTTP 400 for invalid payload or invalid search business input;
            NOT_FOUND (404) with HTTP 404 for missing or hidden datasets;
            EXCEPTION_ERROR with HTTP 502 for runtime dependency failures.
    """
    req, err = await validate_and_parse_json_request(request, SearchDatasetsReq)
    if err is not None:
        return _get_search_argument_error_result(err)
    try:
        success, result = await dataset_api_service.search_datasets(scope_id, req)
        if success:
            return get_result(data=result)
        else:
            return _get_search_business_error_result(result)
    except Exception as e:
        logging.exception(e)
        return _get_search_dependency_error_result()


@manager.route("/datasets/<dataset_id>/search", methods=["POST"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def search(scope_id, dataset_id):
    """Search (retrieval test) within a dataset.

    POST /api/v1/datasets/<dataset_id>/search
    JSON body: {"question": str (required), "doc_ids": list[str], "top_k": int, "page": int, "size": int,
               "similarity_threshold": float, "vector_similarity_weight": float, "use_kg": bool,
               "cross_languages": list[str], "keyword": bool, "meta_data_filter": dict}
    Success: {"code": 0, "data": {"chunks": [...], "total": int, "labels": [...]}}
    Errors: ARGUMENT_ERROR (101) with HTTP 400 for invalid payload or invalid search business input;
            NOT_FOUND (404) with HTTP 404 for missing or hidden datasets;
            EXCEPTION_ERROR with HTTP 502 for runtime dependency failures.
    """
    req, err = await validate_and_parse_json_request(request, SearchDatasetReq)
    if err is not None:
        return _get_search_argument_error_result(err)
    req['dataset_ids'] = [dataset_id]
    try:
        success, result = await dataset_api_service.search_datasets(scope_id, req)
        if success:
            return get_result(data=result)
        else:
            return _get_search_business_error_result(result)
    except Exception as e:
        logging.exception(e)
        return _get_search_dependency_error_result()


@manager.route("/datasets/<dataset_id>/graph", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def get_knowledge_graph(scope_id, dataset_id):
    """Get the knowledge graph of a dataset.

    GET /api/v1/datasets/<dataset_id>/graph
    Query params: optional filter params.
    Success: {"code": 0, "data": {...}}
    Errors: AUTHENTICATION_ERROR for access denied; DATA_ERROR for internal errors.
    """
    try:
        success, result = await dataset_api_service.get_knowledge_graph(dataset_id, scope_id)
        if success:
            return get_result(data=result)
        else:
            return get_result(data=False, message=result, code=RetCode.AUTHENTICATION_ERROR)
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/index", methods=["POST"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def run_index(scope_id, dataset_id):
    index_type = request.args.get("type", "")
    index_type = index_type.lower()
    try:
        success, result = dataset_api_service.run_index(dataset_id, scope_id, index_type)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/index", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def trace_index(scope_id, dataset_id):
    index_type = request.args.get("type", "")
    index_type = index_type.lower()
    try:
        success, result = dataset_api_service.trace_index(dataset_id, scope_id, index_type)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/<index_type>", methods=["DELETE"])  # noqa: F821
@manager.route("/datasets/<dataset_id>/index", methods=["DELETE"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def delete_index(scope_id, dataset_id, index_type=None):
    index_type = (index_type or request.args.get("type", "")).lower()
    if index_type not in dataset_api_service._VALID_INDEX_TYPES:
        return get_error_argument_result(f"Invalid index type '{index_type}'")
    # `wipe` controls whether the persisted index artefacts (graph rows /
    # raptor summaries) are removed. Default true preserves historical
    # behaviour; pass wipe=false to cancel the running task while keeping
    # prior progress so it can be resumed later.
    wipe_arg = (request.args.get("wipe", "true") or "true").strip().lower()
    wipe = wipe_arg not in ("false", "0", "no", "off")
    try:
        success, result = dataset_api_service.delete_index(dataset_id, scope_id, index_type, wipe=wipe)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/embedding", methods=["POST"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def run_embedding(scope_id, dataset_id):
    try:
        success, result = dataset_api_service.run_embedding(dataset_id, scope_id)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/embedding/check", methods=["POST"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def check_embedding(scope_id, dataset_id):
    try:
        req = await request.get_json()
        if not req or not req.get("embd_id"):
            return get_error_data_result(message="`embd_id` is required.")
        status, result = dataset_api_service.check_embedding(dataset_id, scope_id, req)
        if status is True:
            return get_result(data=result)
        elif status == "not_effective":
            return get_json_result(code=result["code"], message=result["message"], data=result["data"])
        else:
            return get_error_data_result(message=result)
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/ingestions", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def list_ingestion_logs(scope_id, dataset_id):
    try:
        page = int(request.args.get("page", 0))
        page_size = validate_rest_api_page_size(int(request.args.get("page_size", 0)))
        orderby = request.args.get("orderby", "create_time")
        desc = request.args.get("desc", "true").lower() != "false"
        operation_status = request.args.getlist("operation_status")
        create_date_from = request.args.get("create_date_from", None)
        create_date_to = request.args.get("create_date_to", None)
        log_type = request.args.get("log_type", "dataset")
        keywords = request.args.get("keywords", None)
        success, result = dataset_api_service.list_ingestion_logs(dataset_id, scope_id, page, page_size, orderby, desc, operation_status, create_date_from, create_date_to, log_type, keywords)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/ingestions/<log_id>", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def get_ingestion_log(scope_id, dataset_id, log_id):
    try:
        success, result = dataset_api_service.get_ingestion_log(dataset_id, scope_id, log_id)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/metadata/config", methods=["GET"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
def get_auto_metadata(scope_id, dataset_id):
    """
    Get auto-metadata configuration for a dataset.
    ---
    tags:
      - Datasets
    security:
      - ApiKeyAuth: []
    parameters:
      - in: path
        name: dataset_id
        type: string
        required: true
        description: ID of the dataset.
      - in: header
        name: Authorization
        type: string
        required: true
        description: Bearer token for authentication.
    responses:
      200:
        description: Successful operation.
        schema:
          type: object
    """
    try:
        success, result = dataset_api_service.get_auto_metadata(dataset_id, scope_id)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")


@manager.route("/datasets/<dataset_id>/metadata/config", methods=["PUT"])  # noqa: F821
@login_required
@add_scope_id_to_kwargs
async def update_auto_metadata(scope_id, dataset_id):
    """
    Update auto-metadata configuration for a dataset.
    ---
    tags:
      - Datasets
    security:
      - ApiKeyAuth: []
    parameters:
      - in: path
        name: dataset_id
        type: string
        required: true
        description: ID of the dataset.
      - in: header
        name: Authorization
        type: string
        required: true
        description: Bearer token for authentication.
      - in: body
        name: body
        description: Auto-metadata configuration.
        required: true
        schema:
          type: object
    responses:
      200:
        description: Successful operation.
        schema:
          type: object
    """
    from api.utils.validation_utils import AutoMetadataConfig

    cfg, err = await validate_and_parse_json_request(request, AutoMetadataConfig)
    if err is not None:
        return get_error_argument_result(err)

    try:
        success, result = await dataset_api_service.update_auto_metadata(dataset_id, scope_id, cfg)
        if success:
            return get_result(data=result)
        else:
            return get_error_data_result(message=result)
    except ValueError as e:
        return get_error_argument_result(str(e))
    except Exception as e:
        logging.exception(e)
        return get_error_data_result(message="Internal server error")
