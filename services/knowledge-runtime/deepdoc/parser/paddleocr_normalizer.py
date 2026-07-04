from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any

from bs4 import BeautifulSoup

from deepdoc.parser.paddleocr_adapter import BBox, EMPTY_BBOX, PaddleOCRBlock, PaddleOCRPage


_MARKDOWN_IMAGE_PATTERN = re.compile(
    r"""
        <div[^>]*>\s*
        <img[^>]*/>\s*
        </div>
        |
        <img[^>]*/>
        |
        !\[[^\]]*]\([^)]*\)
        """,
    re.IGNORECASE | re.VERBOSE | re.DOTALL,
)
_HTML_TABLE_PATTERN = re.compile(r"<table\b[^>]*>.*?</table>", re.IGNORECASE | re.DOTALL)
_HTML_TAG_PATTERN = re.compile(r"<[^>]+>")


@dataclass
class SemanticSection:
    text: str
    block_type: str
    page_number: int
    bbox: BBox = EMPTY_BBOX
    metadata: dict[str, Any] = field(default_factory=dict)

    def position_tag(self, zoomin: int) -> str:
        left, top, right, bottom = self.bbox
        if zoomin <= 0:
            zoomin = 1
        return f"@@{self.page_number}\t{int(left // zoomin)}\t{int(right // zoomin)}\t{int(top // zoomin)}\t{int(bottom // zoomin)}##"

    def to_section_tuple(self, parse_method: str, zoomin: int) -> tuple[str, ...]:
        tag = self.position_tag(zoomin)
        if parse_method in {"manual", "pipeline"}:
            return (self.text, self.block_type, tag)
        if parse_method == "paper":
            return (self.text + tag, self.block_type)
        return (self.text, tag)


class PaddleOCRLayoutNormalizer:
    """Normalize PaddleOCR layout blocks into chunker-friendly sections."""

    def normalize(self, pages: list[PaddleOCRPage]) -> list[SemanticSection]:
        sections: list[SemanticSection] = []
        for page in pages:
            if page.blocks:
                for block in page.blocks:
                    sections.extend(self._normalize_block(block))
            elif page.markdown.strip():
                sections.extend(self._normalize_block(PaddleOCRBlock(page.page_number, "markdown", page.markdown)))
        return sections

    def _normalize_block(self, block: PaddleOCRBlock) -> list[SemanticSection]:
        text = self._clean_text(block.text, block.block_type)
        if not text:
            return []

        if block.block_type == "markdown":
            return [
                SemanticSection(
                    text=part,
                    block_type=self._infer_text_block_type(part),
                    page_number=block.page_number,
                    bbox=block.bbox,
                    metadata={**block.metadata, "normalized_from": "markdown"},
                )
                for part in self._split_markdown(text)
                if part.strip()
            ]

        return [
            SemanticSection(
                text=text,
                block_type=block.block_type,
                page_number=block.page_number,
                bbox=block.bbox,
                metadata={**block.metadata, "normalized_from": "layout_block"},
            )
        ]

    def _clean_text(self, text: str, block_type: str) -> str:
        text = _remove_images_from_markdown(text or "")
        if not text.strip():
            return ""

        if block_type == "table" or "<table" in text.lower():
            text = self._html_tables_to_markdown(text)
        elif "<" in text and ">" in text:
            text = self._html_to_text(text)

        text = self._normalize_whitespace(text)
        if block_type == "heading":
            text = self._normalize_heading(text)
        return text.strip()

    def _html_tables_to_markdown(self, text: str) -> str:
        return _HTML_TABLE_PATTERN.sub(lambda match: self._table_to_markdown(match.group(0)), text)

    def _table_to_markdown(self, html_table: str) -> str:
        soup = BeautifulSoup(html_table, "html.parser")
        table = soup.find("table")
        if table is None:
            return self._html_to_text(html_table)

        rows: list[list[str]] = []
        for row in table.find_all("tr"):
            cells = row.find_all(["th", "td"])
            if not cells:
                continue
            rows.append([self._escape_table_cell(cell.get_text(" ", strip=True)) for cell in cells])

        if not rows:
            return self._html_to_text(html_table)

        width = max(len(row) for row in rows)
        normalized_rows = [row + [""] * (width - len(row)) for row in rows]
        header = normalized_rows[0]
        body = normalized_rows[1:]
        lines = [
            "| " + " | ".join(header) + " |",
            "| " + " | ".join("---" for _ in header) + " |",
        ]
        lines.extend("| " + " | ".join(row) + " |" for row in body)
        return "\n".join(lines)

    @staticmethod
    def _escape_table_cell(text: str) -> str:
        return re.sub(r"\s+", " ", text).replace("|", "\\|").strip()

    @staticmethod
    def _html_to_text(text: str) -> str:
        soup = BeautifulSoup(text, "html.parser")
        return soup.get_text(" ", strip=True)

    @staticmethod
    def _normalize_heading(text: str) -> str:
        stripped = text.strip()
        if re.match(r"^#{1,6}\s+\S", stripped):
            return stripped
        return f"## {stripped}"

    @staticmethod
    def _infer_text_block_type(text: str) -> str:
        stripped = text.lstrip()
        if re.match(r"^#{1,6}\s+\S", stripped):
            return "heading"
        if stripped.startswith("|") and "\n| ---" in stripped:
            return "table"
        if stripped.startswith("$$"):
            return "formula"
        return "paragraph"

    @staticmethod
    def _split_markdown(text: str) -> list[str]:
        parts: list[str] = []
        current: list[str] = []
        in_table = False
        for line in text.splitlines():
            starts_heading = bool(re.match(r"^#{1,6}\s+\S", line))
            starts_table = line.startswith("|")
            if starts_heading and current:
                parts.append("\n".join(current).strip())
                current = []
            elif starts_table and not in_table and current:
                parts.append("\n".join(current).strip())
                current = []
            elif not starts_table and in_table and current:
                parts.append("\n".join(current).strip())
                current = []
            current.append(line)
            in_table = starts_table
        if current:
            parts.append("\n".join(current).strip())
        return [part for part in parts if part.strip()]

    @staticmethod
    def _normalize_whitespace(text: str) -> str:
        text = text.replace("\r\n", "\n").replace("\r", "\n")
        text = re.sub(r"[ \t]+\n", "\n", text)
        text = re.sub(r"\n{3,}", "\n\n", text)
        return text.strip()


def _remove_images_from_markdown(markdown: str) -> str:
    return _MARKDOWN_IMAGE_PATTERN.sub("", markdown)
