#!/usr/bin/env bash

# HO.

# Copyright © 2020 Alper Rifat Ulucinar
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

if [[ "${KREW_COMMIT:-}" != "true" ]]; then
  echo "Not committing changes in krew manifest..."

  exit 0
fi

git add plugins/edit-status.yaml
git commit -m "chore: krew manifest for release ${TAG}"
git push origin