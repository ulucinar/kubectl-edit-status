#!/usr/bin/env bash

# HO.

# Copyright © 2021 Alper Rifat Ulucinar
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euxo pipefail

export SHA_DARWIN_AMD64="$(sha256sum "dist/${PROJECT_NAME}_${TAG}_darwin_amd64.tar.gz" | cut -d' ' -f1)"
export SHA_DARWIN_ARM64="$(sha256sum "dist/${PROJECT_NAME}_${TAG}_darwin_arm64.tar.gz" | cut -d' ' -f1)"
export SHA_LINUX_AMD64="$(sha256sum "dist/${PROJECT_NAME}_${TAG}_linux_amd64.tar.gz" | cut -d' ' -f1)"
export SHA_WINDOWS_AMD64="$(sha256sum "dist/${PROJECT_NAME}_${TAG}_windows_amd64.tar.gz" | cut -d' ' -f1)"

# prepare krew plugin manifest
envsubst < krew/edit-status.yaml.template > plugins/edit-status.yaml
# commit krew plugin manifest if requested
krew/commit_krew_manifest.sh
