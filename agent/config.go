package agent

import (
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/naoina/toml"
)

type (
	OnExit   string
	Duration time.Duration
)

const (
	OnExitRestart OnExit = "restart"
	OnExitIgnore  OnExit = "ignore"
	OnExitProxy   OnExit = "proxy"
)

//Config is shared for both toml unmarshalling and opts CLI generation.
//Defaults are applied on ValidateConfig.
type Config struct {
	Host               string   `opts:"help=listening interface, default=0.0.0.0"`
	Port               int      `opts:"help=listening port, default=8080, env=PORT"`
	ProgramArgs        []string `opts:"mode=arg, name=arg, help=args can be either a command with arguments or a webproc file, min=1"`
	OnExit             OnExit   `opts:"help=process exit action, short=o, default=ignore"`
	ConfigurationFiles []string `opts:"mode=flag, help=writable configuration file"`
	RestartWatch       bool     `opts:"short=w,help=changing config files on disk triggers a restart"`
	RestartTimeout     Duration `opts:"help=restart timeout controls when to perform a force kill, default=30s"`
}

func LoadConfig(path string, c *Config) error {
	if info, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("file not found")
	} else if err != nil {
		return fmt.Errorf("file error: %s", err)
	} else if info.IsDir() {
		return fmt.Errorf("file not specified")
	}
	cbuff, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("file read error: %s", err)
	}
	//toml reader
	if err := toml.Unmarshal(cbuff, c); err != nil {
		return fmt.Errorf("toml syntax error: %s", err)
	}
	return nil
}

func ValidateConfig(c *Config) error {
	if len(c.ProgramArgs) == 0 {
		return fmt.Errorf("required property ProgramArgs is missing")
	}
	//apply defaults
	if c.Host == "" {
		c.Host = "0.0.0.0"
	}
	if c.Port == 0 {
		c.Port = 8080
	}
	switch c.OnExit {
	case OnExitProxy, OnExitIgnore, OnExitRestart:
		//valid
	case "":
		c.OnExit = OnExitIgnore
	default:
		return fmt.Errorf("on-exit option must be 'proxy' 'ignore' or 'restart'")
	}
	if c.RestartTimeout <= 0 {
		c.RestartTimeout = Duration(30 * time.Second)
	}
	return nil
}

// helper types

func (o *OnExit) UnmarshalTOML(data []byte) error {
	*o = OnExit(quoted(data))
	return nil
}

func (o *OnExit) Set(s string) error {
	*o = OnExit(s)
	return nil
}

func (o *OnExit) String() string {
	return string(*o)
}

func (d *Duration) UnmarshalTOML(data []byte) error {
	str := quoted(data)
	d2, err := time.ParseDuration(str)
	*d = Duration(d2)
	return err
}

func quoted(data []byte) string {
	if l := len(data); l >= 2 {
		return string(data[1 : l-1])
	}
	return string(data)
}
