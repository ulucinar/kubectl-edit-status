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

GO_NO_CGO := CGO_ENABLED=0 go
TAG ?= $(shell git describe --tags --abbrev=0)
PLUGIN_BINARY := kubectl-edit_status
PROJECT_NAME := kubectl-edit-status

.PHONY: clean krew all

# build kubectl-edit-status binary
kubectl-edit_status-%:
	echo "TAG: $(TAG)"
	$(eval executable := $(PLUGIN_BINARY)$(EXTENSION))
	$(eval archive_folder_% := dist/$(PROJECT_NAME)_$(TAG)_$(GOOS)_$(GOARCH))
	GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO_NO_CGO) build -o dist/$(executable) main.go
	mkdir $(archive_folder_%)
	cp dist/$(executable) $(archive_folder_%)
	cp LICENSE* $(archive_folder_%)
	tar czvf $(archive_folder_%).tar.gz --strip-components 2 $(archive_folder_%)
	rm -f dist/$(executable)
	rm -fR $(archive_folder_%)

bin-darwin: GOOS = darwin
bin-darwin: GOARCH = amd64
bin-darwin: kubectl-edit_status-darwin

bin-linux: GOOS = linux
bin-linux: GOARCH = amd64
bin-linux: kubectl-edit_status-linux

bin-windows: GOOS = windows
bin-windows: GOARCH = amd64
bin-windows: EXTENSION = .exe
bin-windows: kubectl-edit_status-windows

archives: bin-darwin bin-linux bin-windows

krew:
	KREW_COMMIT=$(KREW_COMMIT) TAG=$(TAG) PLUGIN_BINARY=$(PLUGIN_BINARY) PROJECT_NAME=$(PROJECT_NAME) krew/prepare_krew_manifest.sh

clean:
	rm -fR dist
	rm -f plugins/edit-status.yaml

all: clean archives krew
