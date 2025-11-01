package defs

import "os"

const (
	MicaConfDir  = "/etc/mica"
	MicaStateDir = "/run/mica"
	DaemonRoot   = "/run"

	DirMode  = os.FileMode(0700) | os.ModeDir
	FileMode = os.FileMode(0644)
)

const (
	MicranContainerStateDir       = "/run/micran"
	DefaultMicaContainersRoot   = "/run/micran/containers"
	MicantainerStateFile = "state.json"
	SandboxStateFile     = "state.json"
	// directory for sandbox data storage
	SandboxDataDir = "/run/micran/sandbox"

	// override priority:
	// file(os.env("MicranConfDir")) > dir(os.env("MicranConfEnv"))/*.toml > dir(MicranConfDir)/conf.toml
	DropinDirEnv = "MICRAN_CONF_DIR"
	// default value of micran conf root directories, if MICRAN_CONF_DIR is not set
	MicranConfDir    = "/etc/micran"
	MicranConfDropin = MicaConfDir + "/conf.d"

	// if file exists, it will override
	MicranConfEnv = "MICRAN_CONF_FILE"
)
