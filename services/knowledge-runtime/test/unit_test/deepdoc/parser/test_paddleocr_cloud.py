import json

from deepdoc.parser.paddleocr_adapter import PaddleOCRResultAdapter
from deepdoc.parser.paddleocr_client import PaddleOCRCloudClient, PaddleOCRCloudRequestConfig
from deepdoc.parser.paddleocr_normalizer import PaddleOCRLayoutNormalizer
from deepdoc.parser.paddleocr_parser import PaddleOCRParser


class _Request:
    def __init__(self, headers):
        self.headers = headers


class _Response:
    def __init__(self, status_code=200, payload=None, text=None, headers=None):
        self.status_code = status_code
        self._payload = payload
        self.text = text if text is not None else json.dumps(payload or {})
        self.request = _Request(headers or {})

    def json(self):
        if self._payload is None:
            raise ValueError("not json")
        return self._payload

    def raise_for_status(self):
        if self.status_code >= 400:
            raise RuntimeError(f"HTTP {self.status_code}")


class _Session:
    def __init__(self):
        self.posts = []
        self.gets = []

    def post(self, url, **kwargs):
        self.posts.append((url, kwargs))
        return _Response(payload={"data": {"jobId": "job-1"}}, headers=kwargs.get("headers"))

    def get(self, url, **kwargs):
        self.gets.append((url, kwargs))
        if url.endswith("/job-1"):
            return _Response(payload={"data": {"state": "done", "resultUrl": {"jsonUrl": "https://result.example/result.jsonl"}}}, headers=kwargs.get("headers"))
        return _Response(
            text=json.dumps(
                {
                    "result": {
                        "layoutParsingResults": [
                            {"markdown": {"text": "# 标题\n\n正文内容"}},
                        ]
                    }
                }
            )
        )


def test_cloud_client_uses_token_auth_and_combines_jsonl_result():
    session = _Session()
    client = PaddleOCRCloudClient(session=session)

    result = client.parse_pdf(
        b"%PDF-1.7",
        PaddleOCRCloudRequestConfig(
            base_url="https://paddleocr.example.com",
            access_token="secret-token",
            algorithm="PaddleOCR-VL",
            auth_scheme="token",
        ),
        {"prettifyMarkdown": True},
    )

    post_headers = session.posts[0][1]["headers"]
    poll_headers = session.gets[0][1]["headers"]
    assert post_headers["Authorization"] == "token secret-token"
    assert poll_headers["Authorization"] == "token secret-token"
    assert session.posts[0][1]["data"]["model"] == "PaddleOCR-VL"
    assert json.loads(session.posts[0][1]["data"]["optionalPayload"]) == {"prettifyMarkdown": True}
    assert result["layoutParsingResults"][0]["markdown"]["text"] == "# 标题\n\n正文内容"


def test_parser_extracts_sections_from_cloud_markdown_text():
    parser = PaddleOCRParser(access_token="secret-token")
    result = {
        "layoutParsingResults": [
            {"markdown": {"text": "# 标题\n\n正文内容\n\n<img src='x'/>"}},
            {"markdownText": "第二页内容"},
        ]
    }

    sections = parser._transfer_to_sections(result, algorithm="PaddleOCR-VL", parse_method="raw")

    assert sections == [
        ("# 标题\n\n正文内容", "@@1\t0\t0\t0\t0##"),
        ("第二页内容", "@@2\t0\t0\t0\t0##"),
    ]


def test_result_adapter_and_normalizer_produce_semantic_table_sections():
    raw = {
        "layoutParsingResults": [
            {
                "prunedResult": {
                    "width": 1000,
                    "height": 1400,
                    "parsing_res_list": [
                        {
                            "block_label": "paragraph_title",
                            "block_content": "4 报废指标",
                            "block_bbox": [100, 200, 500, 240],
                            "block_order": 1,
                        },
                        {
                            "block_label": "table",
                            "block_content": "<table><tr><td>项目</td><td>指标</td></tr><tr><td>含水量%</td><td>≥60</td></tr></table>",
                            "block_bbox": [100, 250, 800, 420],
                            "block_order": 2,
                        },
                    ],
                }
            }
        ]
    }

    pages = PaddleOCRResultAdapter().adapt(raw)
    sections = PaddleOCRLayoutNormalizer().normalize(pages)

    assert [section.block_type for section in sections] == ["heading", "table"]
    assert sections[0].text == "## 4 报废指标"
    assert sections[1].text == "| 项目 | 指标 |\n| --- | --- |\n| 含水量% | ≥60 |"
    assert sections[1].metadata["raw_block_type"] == "table"


def test_parser_renders_normalized_layout_blocks_to_legacy_section_tuples():
    parser = PaddleOCRParser(access_token="secret-token")
    raw = {
        "layoutParsingResults": [
            {
                "prunedResult": {
                    "parsing_res_list": [
                        {
                            "block_label": "table",
                            "block_content": "<table><tr><td>A</td><td>B</td></tr><tr><td>1</td><td>2</td></tr></table>",
                            "block_bbox": [10, 20, 50, 80],
                        }
                    ]
                }
            }
        ]
    }

    sections = parser._transfer_to_sections(raw, algorithm="PaddleOCR-VL", parse_method="manual")

    assert sections == [
        ("| A | B |\n| --- | --- |\n| 1 | 2 |", "table", "@@1\t5\t25\t10\t40##")
    ]


def test_parser_config_accepts_bearer_override():
    headers = PaddleOCRCloudClient()._headers(
        PaddleOCRCloudRequestConfig(
            base_url="https://paddleocr.example.com",
            access_token="secret-token",
            algorithm="PaddleOCR-VL",
            auth_scheme="bearer",
        )
    )

    assert headers["Authorization"] == "Bearer secret-token"
