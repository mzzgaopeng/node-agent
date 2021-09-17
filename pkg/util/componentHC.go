package util

import (
	"errors"
	"go.uber.org/zap"
	"net"
	"net/http"
	"regexp"
	"sync"
	"time"
)

type ProbeType string
type ProbeStatus string
type Probe struct {
	Name      string
	Category  ProbeType
	Address   string
	Duration  int
	Retry     int
	Type      chan bool
	Result    bool
	ProcState ProcessState
}

type ProcessState struct {
	Path string
	Pid  string
}

const (
	Probe_UNIX ProbeType = "unix"
	Probe_HTTP ProbeType = "http"
	Probe_EXEC ProbeType = "exec"
)

func unixHealthProbe(probe Probe) bool {
	var (
		timeout  int  = probe.Duration
		retries  int  = probe.Retry
		isHealth bool = true
		c        net.Conn
		err      error
		logger   = zap.L()
	)

	for retries > 0 {

		dial := net.Dialer{
			Timeout:   time.Duration(timeout) * time.Second,
			KeepAlive: time.Duration(30) * time.Second,
		}
		c, err = dial.Dial("unix", probe.Address)

		if err != nil {
			logger.Error("Fail to connect, " + err.Error())
			if match, _ := regexp.MatchString("timeout", err.Error()); !match {
				time.Sleep(time.Duration(probe.Duration) * time.Second)
			}
			retries -= 1
			isHealth = false
		} else {
			defer c.Close()
			isHealth = true
			break
		}
	}

	return isHealth
}

func httpHealthProbe(probe Probe) bool {
	var (
		timeout   int = probe.Duration
		response  *http.Response
		err       error
		retries   int  = probe.Retry
		isHealth  bool = true
		transport      = http.Transport{
			DisableKeepAlives: true,
		}
		client = http.Client{
			Transport: &transport,
			Timeout:   time.Duration(timeout) * time.Second,
		}
		logger = zap.L()
	)

	for retries > 0 {
		response, err = client.Get(probe.Address)
		if err != nil {
			logger.Error("http GET Error: " + err.Error())
			if match, _ := regexp.MatchString("timeout", err.Error()); !match {
				time.Sleep(time.Duration(timeout) * time.Second)
			}
			retries -= 1
			isHealth = false
		} else {
			defer response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 400 {
				isHealth = true
				break
			} else {
				retries -= 1
				isHealth = false
				time.Sleep(time.Duration(probe.Duration) * time.Second)
			}
		}
	}

	return isHealth
}

func execProbe(probe Probe, p *sync.Map) bool {
	var (
		retries  int  = probe.Retry
		isHealth bool = true
	)

	for retries > 0 {
		execHealth, _ := execProbeCommand(probe, p)
		if execHealth {
			isHealth = true
			break
		} else {
			retries -= 1
			isHealth = false
			time.Sleep(time.Duration(probe.Duration) * time.Second)
		}
	}

	return isHealth
}

func HealthProbe(probe Probe, arg ...*sync.Map) (bool, error) {
	switch probe.Category {
	case Probe_UNIX:
		return unixHealthProbe(probe), nil
	case Probe_HTTP:
		return httpHealthProbe(probe), nil
	case Probe_EXEC:
		return execProbe(probe, arg[0]), nil
	default:
		return false, errors.New("Unknown Health Category:" + string(probe.Category))
	}
}

func execProbeCommand(probe Probe, p *sync.Map) (bool, error) {
	switch probe.Name {
	case "containerd":
		return statExec(probe, p), nil
	case "docker":
		return statExec(probe, p), nil
	case "kubelet":
		return statExec(probe, p), nil
	case "confd":
		return statExec(probe, p), nil
	case "bird":
		return statExec(probe, p), nil
	case "felix":
		return statExec(probe, p), nil
	case "edgeproxy":
		return statExec(probe, p), nil
	default:
		return false, errors.New("Unknown exec name:" + probe.Name)
	}
}
