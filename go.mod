module github.com/ulucinar/kubectl-edit-status

go 1.13

require (
	github.com/evanphx/json-patch v4.12.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	k8s.io/apimachinery v0.23.2
	k8s.io/cli-runtime v0.23.2
	k8s.io/client-go v0.23.2
	sigs.k8s.io/controller-runtime v0.11.0
	sigs.k8s.io/yaml v1.3.0
)
