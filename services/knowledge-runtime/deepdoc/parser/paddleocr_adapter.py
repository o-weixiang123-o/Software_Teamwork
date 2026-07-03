from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


BBox = tuple[float, float, float, float]
EMPTY_BBOX: BBox = (0.0, 0.0, 0.0, 0.0)


def normalize_bbox(value: Any) -> BBox:
    if not isinstance(value, (list, tuple)) or len(value) < 4:
        return EMPTY_BBOX
    try:
        left, top, right, bottom = (float(value[0]), float(value[1]), float(value[2]), float(value[3]))
    except (TypeError, ValueError):
        return EMPTY_BBOX
    if left > right:
        left, right = right, left
    if top > bottom:
        top, bottom = bottom, top
    return left, top, right, bottom


@dataclass
class PaddleOCRBlock:
    page_number: int
    block_type: str
    text: str
    bbox: BBox = EMPTY_BBOX
    order: int = 0
    metadata: dict[str, Any] = field(default_factory=dict)


@dataclass
class PaddleOCRPage:
    page_number: int
    width: float | None = None
    height: float | None = None
    markdown: str = ""
    blocks: list[PaddleOCRBlock] = field(default_factory=list)


class PaddleOCRResultAdapter:
    """Adapt PaddleOCR response variants into ordered page/block records."""

    def adapt(self, result: dict[str, Any]) -> list[PaddleOCRPage]:
        pages = self._layout_pages(result)
        if pages:
            return pages
        return self._ocr_pages(result)

    def _layout_pages(self, result: dict[str, Any]) -> list[PaddleOCRPage]:
        layout_results = result.get("layoutParsingResults", [])
        if not isinstance(layout_results, list):
            return []

        pages: list[PaddleOCRPage] = []
        for page_idx, layout_result in enumerate(layout_results):
            if not isinstance(layout_result, dict):
                continue
            pruned = layout_result.get("prunedResult") if isinstance(layout_result.get("prunedResult"), dict) else {}
            page = PaddleOCRPage(
                page_number=page_idx + 1,
                width=self._number(pruned.get("width")),
                height=self._number(pruned.get("height")),
                markdown=self._markdown_text(layout_result),
            )

            blocks = pruned.get("parsing_res_list", [])
            if isinstance(blocks, list):
                for sequence, raw_block in enumerate(blocks):
                    if not isinstance(raw_block, dict):
                        continue
                    text = str(raw_block.get("block_content") or "")
                    if not text.strip():
                        continue
                    raw_label = str(raw_block.get("block_label") or "")
                    order = self._block_order(raw_block, sequence)
                    page.blocks.append(
                        PaddleOCRBlock(
                            page_number=page.page_number,
                            block_type=self._semantic_block_type(raw_label, text),
                            text=text,
                            bbox=normalize_bbox(raw_block.get("block_bbox")),
                            order=order,
                            metadata={
                                "source": "layoutParsingResults.prunedResult.parsing_res_list",
                                "raw_block_type": raw_label,
                                "block_id": raw_block.get("block_id"),
                                "block_order": raw_block.get("block_order"),
                                "group_id": raw_block.get("group_id"),
                            },
                        )
                    )

            if not page.blocks and page.markdown.strip():
                page.blocks.append(
                    PaddleOCRBlock(
                        page_number=page.page_number,
                        block_type="markdown",
                        text=page.markdown,
                        order=0,
                        metadata={"source": "layoutParsingResults.markdown"},
                    )
                )
            pages.append(page)

        return pages

    def _ocr_pages(self, result: dict[str, Any]) -> list[PaddleOCRPage]:
        ocr_results = result.get("ocrResults", [])
        if not isinstance(ocr_results, list):
            return []

        pages: list[PaddleOCRPage] = []
        for page_idx, ocr_result in enumerate(ocr_results):
            if not isinstance(ocr_result, dict):
                continue
            pruned = ocr_result.get("prunedResult") if isinstance(ocr_result.get("prunedResult"), dict) else {}
            texts = pruned.get("rec_texts", [])
            boxes = pruned.get("rec_boxes", [])
            page = PaddleOCRPage(page_number=page_idx + 1)
            if isinstance(texts, list):
                for sequence, text in enumerate(texts):
                    text = str(text or "")
                    if not text.strip():
                        continue
                    bbox = boxes[sequence] if isinstance(boxes, list) and sequence < len(boxes) else EMPTY_BBOX
                    page.blocks.append(
                        PaddleOCRBlock(
                            page_number=page.page_number,
                            block_type="paragraph",
                            text=text,
                            bbox=normalize_bbox(bbox),
                            order=sequence,
                            metadata={"source": "ocrResults.prunedResult.rec_texts"},
                        )
                    )
            pages.append(page)

        return pages

    @staticmethod
    def _markdown_text(layout_result: dict[str, Any]) -> str:
        markdown = layout_result.get("markdown")
        if isinstance(markdown, dict):
            text = markdown.get("text")
            if isinstance(text, str):
                return text
        text = layout_result.get("markdownText") or layout_result.get("markdown")
        return text if isinstance(text, str) else ""

    @staticmethod
    def _block_order(raw_block: dict[str, Any], sequence: int) -> int:
        for key in ("block_order", "block_id"):
            value = raw_block.get(key)
            if isinstance(value, int):
                return value
            if isinstance(value, str) and value.isdigit():
                return int(value)
        return sequence

    @staticmethod
    def _number(value: Any) -> float | None:
        try:
            return float(value)
        except (TypeError, ValueError):
            return None

    @staticmethod
    def _semantic_block_type(raw_label: str, text: str) -> str:
        label = raw_label.strip().lower()
        stripped = text.lstrip()
        if label in {"doc_title", "paragraph_title", "title", "header"} or stripped.startswith("#"):
            return "heading"
        if label in {"table", "table_title"} or "<table" in stripped.lower():
            return "table"
        if label in {"formula", "equation"} or stripped.startswith("$$"):
            return "formula"
        if label in {"figure_title", "caption"}:
            return "caption"
        if label in {"image", "figure", "chart"}:
            return "image"
        if label == "list":
            return "list"
        return "paragraph"
