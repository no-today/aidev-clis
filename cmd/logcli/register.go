package main

import (
	aliyunsls "github.com/no-today/aidev-clis/adapters/log/aliyun_sls"
	"github.com/no-today/aidev-clis/adapters/log/docker"
	"github.com/no-today/aidev-clis/adapters/log/kubectl"
	localfile "github.com/no-today/aidev-clis/adapters/log/local_file"
	sshdocker "github.com/no-today/aidev-clis/adapters/log/ssh_docker"
	sshfile "github.com/no-today/aidev-clis/adapters/log/ssh_file"
	"github.com/no-today/aidev-clis/internal/logcli"
)

// builtins is the ONLY place adapters are wired in. Retire an adapter = delete
// its line here + delete its package dir.
func builtins() []logcli.Adapter {
	return []logcli.Adapter{
		localfile.New(),
		kubectl.New(),
		docker.New(),
		sshfile.New(),
		sshdocker.New(),
		aliyunsls.New(),
	}
}
