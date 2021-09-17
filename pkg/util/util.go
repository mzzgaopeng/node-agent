package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	ud "github.com/ahl5esoft/golang-underscore"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"io/ioutil"
	"os"
	"os/exec"
	//"gopkg.in/natefinch/lumberjack.v2"
	"harmonycloud.cn/agents/node-agent/pkg/lock"
	hleasev1 "harmonycloud.cn/common/hlease/pkg/apis/hlease/v1alpha1"
	clientset "harmonycloud.cn/common/hlease/pkg/client/clientset/versioned"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// TaintByNodeAgent specifies the key the NodeAgent uses to taint nodes as MARKED
	TaintByNodeAgent = "harmonycloud.cn/ErrorNode"
)

type ZkPodUnion struct {
	ZkAddr []string
	PodIP  []string
}

type Protocol string

type SafeMap struct {
	sync.RWMutex
	Map map[string]interface{}
}

const (
	Protocol_HTTP  Protocol = "HTTP"
	Protocol_HTTPS Protocol = "HTTPS"
)

func convertProtocol(protocol Protocol) string {
	switch protocol {
	case Protocol_HTTP:
		return "http"
	case Protocol_HTTPS:
		return "https"
	default:
		return "unknown"
	}
}

// AddToBeRemovedTaint takes a k8s node and adds the ToBeRemovedByAutoscaler taint to the node
// returns the most recent update of the node that is successful
func AddErrorTaintToNode(node *apiv1.Node, client kubernetes.Interface, taintEffect apiv1.TaintEffect) (*apiv1.Node, error) {
	logger := zap.L()

	// fetch the latest version of the node to avoid conflict
	updatedNode, err := client.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
	if err != nil || updatedNode == nil {
		return node, fmt.Errorf("failed to get node %v: %v", node.Name, err)
	}

	// check if the taint already exists
	var taintExists bool
	for _, taint := range updatedNode.Spec.Taints {
		if taint.Key == TaintByNodeAgent {
			taintExists = true
			break
		}
	}

	// don't need to re-add the taint
	if taintExists {
		logger.Warn(TaintByNodeAgent + " already present on node " + updatedNode.Name)
		return updatedNode, nil
	}

	effect := apiv1.TaintEffectNoSchedule
	if len(taintEffect) > 0 {
		effect = taintEffect
	}

	updatedNode.Spec.Taints = append(updatedNode.Spec.Taints, apiv1.Taint{
		Key:    TaintByNodeAgent,
		Value:  fmt.Sprint(time.Now().Unix()),
		Effect: effect,
	})

	patchData := map[string]interface{}{"spec": map[string]interface{}{"taints": updatedNode.Spec.Taints}}
	patchDataBytes, _ := json.Marshal(patchData)
	updatedNodeWithTaint, err := client.CoreV1().Nodes().Patch(node.Name, types.MergePatchType, patchDataBytes)
	if err != nil || updatedNodeWithTaint == nil {
		return updatedNode, fmt.Errorf("failed to update node %v after adding taint: %v", updatedNode.Name, err)
	}

	logger.Info("Successfully added taint on node " + updatedNodeWithTaint.Name)

	return updatedNodeWithTaint, nil
}

func FetchLock(etcdips []string, ttl int64, nodename string, protocol Protocol,
	certfile string, cafile string, keyfile string) bool {
	logger := zap.L()

	for index, etcdip := range etcdips {
		etcdips[index] = convertProtocol(protocol) + "://" + etcdip
	}
	eMutex := lock.MakeEtcdMutex(etcdips, ttl, certfile, cafile, keyfile)
	err, _ := eMutex.Lock(nodename)

	if err != nil {
		logger.Error("Fetch lock fails " + err.Error())
		return false
	} else {
		logger.Info("Fetch lock success")
		return true
	}
}

func EventRecorder(client *kubernetes.Clientset, name string, message string, reason string) {
	logger := zap.L()
	node, err := client.CoreV1().Nodes().Get(name, metav1.GetOptions{})

	if err != nil {
		logger.Error(err.Error())
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: v1core.New(client.CoreV1().RESTClient()).Events(""),
	})
	eventRecorder := eventBroadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "node-agent"})
	ref := &apiv1.ObjectReference{
		Kind:      "Node",
		Name:      node.Name,
		UID:       types.UID(node.Name),
		Namespace: "",
	}
	eventRecorder.Eventf(ref, apiv1.EventTypeWarning, reason, message)
	//time.Sleep(time.Second * 10)
}

func GetZKAddr(client *kubernetes.Clientset, nodeName string) map[string][]string {
	logger := zap.L()
	dubboLabels := "monitor-type-thread-dubbo-pool=enable"
	zkPodUnionMap := make(map[string][]string)

	pods, err := client.CoreV1().Pods("").List(metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName, LabelSelector: dubboLabels})
	if err != nil {
		logger.Error("List Pods of nodeName " + nodeName + " error: " + err.Error())
	} else {
		logger.Info("pods: ", zap.Any("items", pods.Items))
		for _, p := range pods.Items {
			podIP := p.Status.PodIP
			if podIP == "" {
				logger.Error("The Pod IP is empty, skip. Pod: " + p.Namespace + "/" + p.Name)
				continue
			}
			zkAddr := GetZkAddress(&p)
			sort.Strings(zkAddr)
			zkKey := strings.Join(zkAddr, ",")
			currentPodsIP := zkPodUnionMap[zkKey]
			zkPodUnionMap[zkKey] = append(currentPodsIP, podIP)
			logger.Info(p.GetName(), zap.Any(p.Spec.NodeName, p.Spec.Containers))
		}
	}

	return zkPodUnionMap
}

func ConvertZkPodUnion(zkPodUnionMap map[string][]string) []ZkPodUnion {
	var zkPodUnions = []ZkPodUnion{}

	for z := range zkPodUnionMap {
		zkPodUnions = append(zkPodUnions, ZkPodUnion{ZkAddr: strings.Split(z, ","), PodIP: zkPodUnionMap[z]})
	}

	return zkPodUnions
}

func GetZkAddress(pod *apiv1.Pod) []string {
	var dubbozkaddress []string
	var cfgzkaddress []string
	flag := false

	for _, container := range pod.Spec.Containers {

		for _, env := range container.Env {
			if env.Name == "CFG_ENABLED" {
				if env.Value == "true" {
					flag = true
				}
			}
			if env.Name == "DUBBO_ZK" {
				if strings.Contains(env.Value, "?") {
					template := strings.Split(env.Value, "?backup=")
					//i := strings.Index(template[0],"/")
					dubbozkaddress = append(dubbozkaddress, strings.TrimLeft(template[0], "zookeeper://"))
					backupad := strings.Split(template[1], ",")
					for _, addr := range backupad {
						dubbozkaddress = append(dubbozkaddress, addr)
					}
				} else {
					dubbozkaddress = append(dubbozkaddress, strings.TrimLeft(env.Value, "zookeeper://"))
				}
			} else if env.Name == "CFG_ZK" {
				template := strings.Split(env.Value, ",")
				for _, addr := range template {
					cfgzkaddress = append(cfgzkaddress, addr)
				}
			}
		}
	}
	if flag == true {
		return cfgzkaddress
	}
	return dubbozkaddress
}

func ReportError(c chan string, client *kubernetes.Clientset, name string, reason string) {
	for {
		c, ok := <-c
		if !ok {
			return
		}
		EventRecorder(client, name, c, reason)
	}
}

func InitHlease(isolateClientSet clientset.Interface, nodeName string) (*hleasev1.HLease, error) {
	hlease, err := isolateClientSet.IsolateV1alpha1().HLeases("default").Get(nodeName, metav1.GetOptions{})

	if err != nil {
		message := err.Error()

		if match, _ := regexp.MatchString("not found", message); match {
			newhlease := hleasev1.HLease{ObjectMeta: metav1.ObjectMeta{
				Name:      nodeName,
				Namespace: "default",
			}, Spec: hleasev1.HLeaseSpec{Switch: "ON", LastHeartbeatTime: metav1.Now()}}

			return isolateClientSet.IsolateV1alpha1().HLeases("default").Create(&newhlease)
		}

		return nil, err
	}

	return hlease, nil
}

func UpdateBeats(isolateClientSet clientset.Interface, nodeName string) {
	logger := zap.L()

	for {
		hlease, err := isolateClientSet.IsolateV1alpha1().HLeases("default").Get(nodeName, metav1.GetOptions{})
		if err != nil {
			logger.Error("get beats fail: " + err.Error())
		}
		hlease.Spec.LastHeartbeatTime = metav1.Now()

		_, err = isolateClientSet.IsolateV1alpha1().HLeases("default").Update(hlease)
		if err != nil {
			logger.Error("update beats fail: " + err.Error())
		}

		time.Sleep(time.Duration(1) * time.Second)
	}
}

func Replay(probe Probe, writeWg *sync.WaitGroup, healthProbe *sync.Map, result bool) {
	probe.Result = result
	newProbe, _ := healthProbe.Load(probe.Name)
	probe.ProcState.Pid = newProbe.(Probe).ProcState.Pid
	healthProbe.Store(probe.Name, probe)
	writeWg.Done()
}

func CheckNodeTaint(node *apiv1.Node) bool {
	hasTaints := false

	for _, taint := range node.Spec.Taints {
		if taint.Key == "harmonycloud.cn/ErrorNode" {
			hasTaints = true
			break
		}
	}

	return hasTaints
}

func DispatchProbe(writeWg *sync.WaitGroup, healthProbe *sync.Map, length int) {
	var (
		res         bool
		healthError error
	)
	writeWg.Add(length)
	logger := zap.L()

	healthProbe.Range(func(k, v interface{}) bool {
		probe := v.(Probe)

		go func(probe Probe, write_wg *sync.WaitGroup, healthProbe *sync.Map) {
			if probe.Category == "exec" {
				res, healthError = HealthProbe(probe, healthProbe)
			} else {
				res, healthError = HealthProbe(probe)
			}
			if healthError != nil {
				logger.Error("healthProbe Error " + healthError.Error())
				Replay(probe, writeWg, healthProbe, false)
				return
			}
			Replay(probe, writeWg, healthProbe, res)
		}(probe, writeWg, healthProbe)

		return true
	})
}

func FormatMessage(healthProbe sync.Map) string {
	message := make([]string, 0)
	probes := make([]Probe, 0)

	healthProbe.Range(func(k, v interface{}) bool {
		p := v.(Probe)
		probes = append(probes, p)

		return true
	})

	ud.Chain(probes).Filter(func(s Probe, _ int) bool {
		return !s.Result
	}).Map(func(s Probe, _ int) string {
		return s.Name + " down"
	}).Value(&message)

	return strings.Join(message, ", ")
}

// logpath 日志文件路径
// loglevel 日志级别
func InitLogger(logpath string, loglevel string, logMaxSize int, logMaxBackups int, logMaxAge int) *zap.Logger {
	var level zapcore.Level

	switch loglevel {
	case "debug":
		level = zap.DebugLevel
	case "info":
		level = zap.InfoLevel
	case "error":
		level = zap.ErrorLevel
	default:
		level = zap.InfoLevel
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder, // 小写编码器
		EncodeTime:     zapcore.ISO8601TimeEncoder,    // ISO8601 UTC 时间格式
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.FullCallerEncoder, // 全路径编码器
	}

	hook := lumberjack.Logger{
		Filename:   logpath,       // 日志文件路径
		MaxSize:    logMaxSize,    // megabytes
		MaxBackups: logMaxBackups, // 最多保留3个备份
		MaxAge:     logMaxAge,     //days
		Compress:   true,          // 是否压缩 disabled by default
	}
	w := zapcore.AddSync(&hook)

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		w,
		level,
	)

	logger := zap.New(core)

	return logger
}

func ChangeSwitch(isolateclientset clientset.Interface, nodeName string) {
	logger := zap.L()
	retries := 3

	for retries > 0 {
		hlease, err := isolateclientset.IsolateV1alpha1().HLeases("default").Get(nodeName, metav1.GetOptions{})
		if err != nil {
			logger.Error("get hlease fails: " + err.Error())
			retries -= 1
			continue
		}
		hlease.Spec.Switch = "OFF"
		_, err = isolateclientset.IsolateV1alpha1().HLeases("default").Update(hlease)
		if err != nil {
			logger.Error("update hlease switch fail: " + err.Error())
			retries -= 1
			continue
		}
		break
	}
}

func Run(timeout int, command string, args ...string) string {

	// instantiate new command
	cmd := exec.Command(command, args...)

	// get pipe to standard output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "cmd.StdoutPipe() error: " + err.Error()
	}

	// start process via command
	if err := cmd.Start(); err != nil {
		return "cmd.Start() error: " + err.Error()
	}

	// setup a buffer to capture standard output
	var buf bytes.Buffer

	// create a channel to capture any errors from wait
	done := make(chan error)
	go func() {
		if _, err := buf.ReadFrom(stdout); err != nil {
			panic("buf.Read(stdout) error: " + err.Error())
		}
		done <- cmd.Wait()
	}()

	// block on select, and switch based on actions received
	select {
	case <-time.After(time.Duration(timeout) * time.Second):
		if err := cmd.Process.Kill(); err != nil {
			return "failed to kill: " + err.Error()
		}
		return "timeout reached, process killed"
	case err := <-done:
		if err != nil {
			close(done)
			return "process done, with error: " + err.Error()
		}
		return "process completed: " + buf.String()
	}
}

func GetProbesPid(healthProbe *sync.Map) {
	healthProbe.Range(func(k, v interface{}) bool {
		p := v.(Probe)
		pid, match := getPidProcess(p)
		if match {
			p.ProcState.Pid = pid
			healthProbe.Store(p.Name, p)
		}

		return true
	})

}

func getPidProcess(probe Probe) (string, bool) {
	logger := zap.L()

	var (
		match bool
		err   error
	)

	d, err := os.Open("/proc")

	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	defer d.Close()

	names, err := d.Readdirnames(-1)
	if err != nil {
		logger.Error("could not read " + d.Name() + err.Error())
	}

	for _, n := range names {
		if match, _ := regexp.MatchString("[0-9]+", n); !match {
			continue
		}
		processState, err := os.Open("/proc/" + n + "/" + probe.ProcState.Path)
		if err != nil {
			logger.Error(err.Error())
		}
		processBytes, err := ioutil.ReadAll(processState)
		if err != nil {
			logger.Error(err.Error())
		}
		procState := string(processBytes)
		if probe.ProcState.Path == "cmdline" {
			match = strings.Contains(procState, probe.Address)
		} else {
			match, _ = regexp.MatchString("\\s"+probe.Address, procState)
			if match && probe.Address == "calico-node" {
				cmdline, err := os.Open("/proc/" + n + "/cmdline")
				if err != nil {
					logger.Error(err.Error())
				}
				cmdlineBytes, err := ioutil.ReadAll(cmdline)
				if err != nil {
					logger.Error(err.Error())
				}

				cmdline.Close()

				match = strings.Contains(string(cmdlineBytes), probe.Name)
			}
		}

		processState.Close()
		if match {
			logger.Info("match process " + probe.Name + ", pid: " + n)
			return n, true
		}
	}
	return "", false
}

// TODO currying exec command, support flatMap and map, use like monad
// lack of abstract let exec use like flow
func statExec(probe Probe, p *sync.Map) bool {
	logger := zap.L()

	// when start node-agent, get process pid fail( not exit probe.Name process or process exit, kubelet, docker, etc...)

	processDetail, err := getProcessDetail("/proc/" + probe.ProcState.Pid + "/" + probe.ProcState.Path)
	if err != nil || (probe.ProcState.Pid == "") {
		pid, match := getPidProcess(probe)
		if !match {
			// process exit
			return false
		}
		logger.Info("Reacquire " + probe.Name + " pid: " + pid)

		processDetail, err = getProcessDetail("/proc/" + pid + "/" + probe.ProcState.Path)
		// can not judge why get /proc/pid/status, so return status
		if err != nil {
			return true
		}
		probe.ProcState.Pid = pid
		p.Store(probe.Name, probe)
	}

	isZombie, _ := regexp.MatchString("zombie", processDetail)

	// process become zombie
	if isZombie {
		return false
	}

	return true
}

func getProcessDetail(path string) (string, error) {
	d, err := os.Open(path)

	if err != nil {
		return "", err
	}

	processDetail, err := ioutil.ReadAll(d)
	defer d.Close()

	return string(processDetail), nil
}

func InitHealthProbe(healthProbe *sync.Map, probes []Probe) {
	for _, probe := range probes {
		healthProbe.Store(probe.Name, probe)
	}
}
