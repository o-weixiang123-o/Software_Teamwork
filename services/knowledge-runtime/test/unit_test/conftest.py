#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
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

"""Shared setup for Knowledge runtime unit tests.

Several parsers and the chunking pipeline tokenize text with NLTK, which needs
the ``punkt_tab`` and ``wordnet`` data sets. Production provisions these via
``download_deps.py`` (into ``nltk_data``, exported as ``NLTK_DATA`` in the
container entrypoint), but the unit-test runner does not run
that provisioning step. Without the data, tokenizer-backed tests such as
``test_epub_parser`` and ``test_dataflow_service`` fail with
``LookupError: Resource 'punkt_tab' not found``. Make sure the data is reachable
before any test imports a tokenizer: reuse a provisioned ``nltk_data`` directory
when present, and download only what is still missing.
"""

import os

import nltk
from nltk.downloader import Downloader

_GITHUB_PROXY_PREFIX = "https://gh-proxy.com/"
_NLTK_DATA_INDEX_URL = "https://raw.githubusercontent.com/nltk/nltk_data/gh-pages/index.xml"
_NLTK_DATA_MIRROR_INDEX_URL = f"{_GITHUB_PROXY_PREFIX}{_NLTK_DATA_INDEX_URL}"
_NLTK_DATA_PACKAGE_PREFIX = "https://raw.githubusercontent.com/nltk/nltk_data/gh-pages/"
_NLTK_DATA_MIRROR_PACKAGE_PREFIX = f"{_GITHUB_PROXY_PREFIX}{_NLTK_DATA_PACKAGE_PREFIX}"

# Reuse data already fetched by download_deps.py (the directory the app exports
# as NLTK_DATA) so provisioned environments do not download it again.
_REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), os.pardir, os.pardir))
_LOCAL_NLTK_DATA = os.path.join(_REPO_ROOT, "ragflow_deps", "nltk_data")
if os.path.isdir(_LOCAL_NLTK_DATA) and _LOCAL_NLTK_DATA not in nltk.data.path:
    nltk.data.path.insert(0, _LOCAL_NLTK_DATA)

# (download name, resource path used by nltk.data.find)
_REQUIRED_NLTK_DATA = (
    ("punkt_tab", "tokenizers/punkt_tab"),
    ("wordnet", "corpora/wordnet"),
)
_NLTK_DOWNLOADER = None


def _nltk_downloader():
    global _NLTK_DOWNLOADER
    if _NLTK_DOWNLOADER is not None:
        return _NLTK_DOWNLOADER

    index_url = os.environ.get("NLTK_DOWNLOAD_INDEX_URL", _NLTK_DATA_INDEX_URL)
    package_prefix = os.environ.get("NLTK_DOWNLOAD_PACKAGE_PREFIX")
    downloader = Downloader(server_index_url=index_url)

    if package_prefix is None and index_url.startswith(_GITHUB_PROXY_PREFIX):
        package_prefix = _NLTK_DATA_MIRROR_PACKAGE_PREFIX

    if package_prefix:
        # NLTK's index embeds raw.githubusercontent.com package URLs. Rewrite
        # those after loading the index so package zip downloads use the same mirror.
        downloader._update_index()
        for package in downloader._packages.values():
            if package.url.startswith(_NLTK_DATA_PACKAGE_PREFIX):
                package.url = package_prefix + package.url[len(_NLTK_DATA_PACKAGE_PREFIX):]

    _NLTK_DOWNLOADER = downloader
    return downloader


for _name, _find_path in _REQUIRED_NLTK_DATA:
    try:
        nltk.data.find(_find_path)
    except LookupError:
        _nltk_downloader().download(_name, quiet=True, raise_on_error=True)
