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
	MicranContainerStateDir   = "/run/micran"
	DefaultMicaContainersRoot = "/run/micran/containers"
	MicantainerStateFile      = "state.json"
	SandboxStateFile          = "state.json"
	// directory for sandbox data storage
	SandboxDataDir = "/run/micran/sandbox"

	// Micrun configuration (INI today, easy to switch to TOML later).
	MicrunConfDir     = "/etc/micrun"
	MicrunConfDropin  = MicrunConfDir + "/conf.d"
	MicrunConfEnv     = "MICRUN_CONF_FILE"
	MicrunConfDirEnv  = "MICRUN_CONF_DIR"
	DefaultMicrunConf = "micrun.ini"
)
