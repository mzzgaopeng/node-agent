package main

import (
	"context"
	"flag"
	"fmt"
	"go.uber.org/zap"
	clientset "harmonycloud.cn/agents/node-agent/pkg/client/clientset/versioned"
	"harmonycloud.cn/agents/node-agent/pkg/util"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//_ "net/http/pprof"
	"k8s.io/client-go/rest"
	"strconv"
	//_ "github.com/mkevac/debugcharts"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"sync"
)

var Version string = "4"

func main() {
	args := os.Args
	if len(args) == 2 && (args[1] == "--version" || args[1] == "-v") {
		fmt.Printf("node-agent version : %s\n", Version)
		return
	}

	var (
		dockerChannel      = make(chan bool)
		kubeletChannel     = make(chan bool)
		containerdChannel  = make(chan bool)
		confdChannel       = make(chan bool)
		birdChannel        = make(chan bool)
		felixChannel       = make(chan bool)
		edgeproxyChannel   = make(chan bool)
		etcdips            string
		ttltime            int64
		kubeConfig         *rest.Config
		crdApiServerConfig *rest.Config
		err                error
		hasTaints          = false
		healthProbe        sync.Map
		probes             = []util.Probe{
			{"docker", "exec", "dockerd", 1, 3, dockerChannel, true, util.ProcessState{Path: "status"}},
			{"containerd", "exec", "/usr/bin/containerd", 1, 3, containerdChannel, true, util.ProcessState{Path: "cmdline"}},
			{"kubelet", "exec", "kubelet", 1, 3, kubeletChannel, true, util.ProcessState{Path: "status"}},
			{"confd", "exec", "confd", 1, 3, confdChannel, true, util.ProcessState{Path: "status"}},
			{"bird", "exec", "bird", 1, 3, birdChannel, true, util.ProcessState{Path: "status"}},
			{"felix", "exec", "calico-felix", 1, 3, felixChannel, true, util.ProcessState{Path: "status"}},
		}
		syncDuration       int
		kubeconfig         string
		write_wg           = sync.WaitGroup{}
		nodeStatus         = true
		deleteResponse     chan string
		isolateclientset   clientset.Interface
		certfile           string
		cafile             string
		keyfile            string
		clusterVersion     string
		protocol           util.Protocol
		logLevel           string
		crdapiserverconfig string
		logPath            string
		logMaxSize         int
		logMaxBackups      int
		logMaxAge          int
		isEdge             bool
		isProxyEdge        bool
		edgeDelayTime      int64
		edgeLastFailedTime time.Time
		edgeContinueFailed bool
		edgeCoreStatus     = true
		edgeProxyStatus    = true
	)

	flag.IntVar(&syncDuration, "syncDuration", 1, "sync duration(seconds)")
	flag.StringVar(&etcdips, "etcdips", "", "etcd master ip")
	flag.StringVar(&clusterVersion, "clusterVersion", "1", "use for different cluster version")
	flag.Int64Var(&ttltime, "ttltime", 3600, "ttl time")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "kube config")
	flag.StringVar(&crdapiserverconfig, "crdApiServerConfig", "", "crd api server config")
	flag.StringVar(&certfile, "certfile", "", "Path to a certfile. ")
	flag.StringVar(&cafile, "cafile", "", "Path to a cafile.")
	flag.StringVar(&keyfile, "keyfile", "", "Path to a keyfile.")
	flag.StringVar(&logLevel, "logLevel", "info", "config log level.")
	flag.StringVar(&logPath, "logPath", "", "store log path.")
	flag.IntVar(&logMaxSize, "logMaxSize", 1024, "maximum size in megabytes of the log file before it gets rotated, default 100MB")
	flag.IntVar(&logMaxBackups, "logMaxBackups", 0, "maximum number of old log files to retain, default retain all")
	flag.IntVar(&logMaxAge, "logMaxAge", 3, "maximum number of days to retain old log files, default retain all")
	flag.BoolVar(&isEdge, "isEdge", false, "indicate whether it is a edge node")
	flag.BoolVar(&isProxyEdge, "isProxyEdge", false, "Indicates whether it is a proxy mode edge node, the value is valid when isEdge = true")
	flag.Int64Var(&edgeDelayTime, "edgeDelayTime", 40, "EdgeCore process is error. delay execute time.")

	flag.Parse()
	logger := util.InitLogger("/var/log/node-agent/node-agent.log", logLevel, logMaxSize, logMaxBackups, logMaxAge)
	if len(logPath) > 0 {
		logger = util.InitLogger(logPath, logLevel, logMaxSize, logMaxBackups, logMaxAge)
	}
	zap.ReplaceGlobals(logger)

	kubeConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		logger.Error("kubeConfig BuildConfigFromFlags error: " + err.Error())
		panic(err)
	}

	crdApiServerConfig, err = clientcmd.BuildConfigFromFlags("", crdapiserverconfig)
	if err != nil {
		logger.Error("crdApiServerConfig BuildConfigFromFlags error: " + err.Error())
		panic(err)
	}

	if len(certfile) > 0 && len(cafile) > 0 && len(keyfile) > 0 {
		protocol = util.Protocol_HTTPS
	} else {
		protocol = util.Protocol_HTTP
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		logger.Error("NewForConfig error: " + err.Error())
		panic(err)
	}

	isolateclientset, err = clientset.NewForConfig(crdApiServerConfig)

	if err != nil {
		logger.Error("isolateclientset NewForConfig error: " + err.Error())
		panic(err)
	}
	nodeName, err := os.Hostname()
	if err != nil {
		logger.Error("get host name error: " + err.Error())
		panic(err)
	}
	//nodeName = "k8s-node4"
	//nodeName = "10.1.11.239-8c16g"

	// report heartbeat
	go util.UpdateBeats(isolateclientset, nodeName)

	if isEdge && isProxyEdge {
		probes = append(probes, util.Probe{"edgeproxy", "exec", "edgeproxy", 1, 3, edgeproxyChannel, true, util.ProcessState{Path: "status"}})
		logger.Info("The node is in proxy mode, add edgeproxy probe.")
	}

	// init get probe pid
	util.InitHealthProbe(&healthProbe, probes)

	if isEdge && !isProxyEdge {
		probe, _ := healthProbe.Load("kubelet")
		kubeletProbe := probe.(util.Probe)
		kubeletProbe.Address = "edgecore"
		healthProbe.Store("kubelet", kubeletProbe)
		logger.Info("change kubelet probe to edgecore probe")
	}
	util.GetProbesPid(&healthProbe)

	logger.Info("ClusterVersion: " + clusterVersion)
	if clusterVersion == "2" {
		healthProbe.Store("felix",
			util.Probe{"felix", "exec", "calico-node", 1, 3, felixChannel, true, util.ProcessState{Path: "status"}})
		healthProbe.Store("confd",
			util.Probe{"confd", "exec", "calico-node", 1, 3, confdChannel, true, util.ProcessState{Path: "status"}})
	}

	for {
		//go func() {
		//	// terminal: $ go tool pprof -http=:8081 http://localhost:6060/debug/pprof/heap
		//	// web:
		//	// 1、http://localhost:8081/ui
		//	// 2、http://localhost:6060/debug/charts
		//	// 3、http://localhost:6060/debug/pprof
		//	http.ListenAndServe("0.0.0.0:6060", nil)
		//}()
		time.Sleep(time.Duration(syncDuration) * time.Second)

		//get hlease or create if not exist(1 node map to 1 hlease)
		//if hlease switch OFF, break this loop
		//(in fact controller will send command to stop node-agent)
		//Judge the switch to prevent unwanted isolation
		hlease, err := util.InitHlease(isolateclientset, nodeName)
		if err != nil {
			logger.Error("init hlease fails: " + err.Error())
			continue
		} else if hlease.Spec.Switch == "OFF" {
			logger.Info("hlease Switch: " + hlease.Spec.Switch)
			continue
		}

		// checkNodeTaint
		node, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})

		if err != nil {
			logger.Error("get Node fails: " + err.Error())
			continue
		}
		hasTaints = util.CheckNodeTaint(node)

		// dispatchProbe
		util.DispatchProbe(&write_wg, &healthProbe, len(probes))
		write_wg.Wait()

		/*
			docker false -> nodeStatus false
			(kubelet + (confd || bird || felix)) 3s -> nodeStatus false
			(containerd + (confd || bird || felix)) 3s -> nodeStatus false
			(confd || bird || felix) 5s -> nodeStatus false
		*/
		dockerState, _ := healthProbe.Load("docker")
		containerdState, _ := healthProbe.Load("containerd")
		kubeletState, _ := healthProbe.Load("kubelet")
		edgeproxyState, _ := healthProbe.Load("edgeproxy")
		confdState, _ := healthProbe.Load("confd")
		birdState, _ := healthProbe.Load("bird")
		felixState, _ := healthProbe.Load("felix")
		calicoState := confdState.(util.Probe).Result && birdState.(util.Probe).Result &&
			felixState.(util.Probe).Result

		nodeStatus = dockerState.(util.Probe).Result && (containerdState.(util.Probe).Result &&
			kubeletState.(util.Probe).Result || calicoState)

		if isEdge && !isProxyEdge {
			if kubeletState.(util.Probe).Result {
				edgeContinueFailed = false
				edgeCoreStatus = true
				logger.Warn("edgecoreStatus was true.")
			} else if !edgeContinueFailed {
				logger.Warn("edgecoreStatus first failed!")
				edgeContinueFailed = true
				edgeLastFailedTime = time.Now()
			} else {
				now := time.Now()
				if now.After(edgeLastFailedTime.Add(time.Duration(edgeDelayTime) * time.Second)) {
					edgeCoreStatus = false
					logger.Warn("edgecoreStatus was false. add errNode taint to node")
				}
			}
		} else if isEdge && isProxyEdge {
			if edgeproxyState.(util.Probe).Result {
				edgeContinueFailed = false
				edgeProxyStatus = true
			} else if !edgeContinueFailed {
				logger.Warn("edgeproxyStatus first failed!")
				edgeContinueFailed = true
				edgeLastFailedTime = time.Now()
			} else {
				now := time.Now()
				if now.After(edgeLastFailedTime.Add(time.Duration(edgeDelayTime) * time.Second)) {
					edgeProxyStatus = false
					logger.Warn("edgeproxyStatus was false. add errNode taint to node")
				}
			}
		}

		if nodeStatus == true && calicoState == false {
			var retrtProbe util.Probe

			if !confdState.(util.Probe).Result {
				retrtProbe = confdState.(util.Probe)
			} else if !birdState.(util.Probe).Result {
				retrtProbe = birdState.(util.Probe)
			} else {
				retrtProbe = felixState.(util.Probe)
			}

			// calico 5s -> isolated
			retrtProbe.Retry = 2
			res, healthError := util.HealthProbe(retrtProbe, &healthProbe)
			if healthError != nil {
				logger.Error("calico Error %v" + healthError.Error())
				nodeStatus = false
			}
			nodeStatus = res
			logger.Info("retry " + retrtProbe.Name + " res: " + strconv.FormatBool(res))
			// recover probe
			retrtProbe.Retry = 3
			retrtProbe.Result = res
			healthProbe.Store(retrtProbe.Name, retrtProbe)
		}

		if isEdge && isProxyEdge {
			logger.Info("docker probe: " + strconv.FormatBool(dockerState.(util.Probe).Result) + ", containerd probe: " + strconv.FormatBool(containerdState.(util.Probe).Result) +
				", kubelet probe: " + strconv.FormatBool(kubeletState.(util.Probe).Result) + ", edgeproxy probe: " + strconv.FormatBool(edgeproxyState.(util.Probe).Result) + ", calico probe: " + "confd: " +
				strconv.FormatBool(confdState.(util.Probe).Result) + ", bird: " + strconv.FormatBool(birdState.(util.Probe).Result) +
				", felix: " + strconv.FormatBool(felixState.(util.Probe).Result))
		} else {
			logger.Info("docker probe: " + strconv.FormatBool(dockerState.(util.Probe).Result) + ", containerd probe: " + strconv.FormatBool(containerdState.(util.Probe).Result) +
				", kubelet probe: " + strconv.FormatBool(kubeletState.(util.Probe).Result) + ", calico probe: " + "confd: " +
				strconv.FormatBool(confdState.(util.Probe).Result) + ", bird: " + strconv.FormatBool(birdState.(util.Probe).Result) +
				", felix: " + strconv.FormatBool(felixState.(util.Probe).Result))
		}

		if nodeStatus == false || !edgeCoreStatus || !edgeProxyStatus {
			// checkNodeTaint
			node, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})

			if err != nil {
				logger.Error("get Node fails: " + err.Error())
				continue
			}
			hasTaints = util.CheckNodeTaint(node)
			//logger.Info("node hasTaints: ", hasTaints)
			logger.Info("node hasTaints: ", zap.Bool("taints", hasTaints))

			if !hasTaints && util.FetchLock(strings.Split(etcdips, ","), ttltime, nodeName, protocol,
				certfile, cafile, keyfile) {

				logger.Info("start node isolate!")

				zkPodUnions := util.ConvertZkPodUnion(util.GetZKAddr(client, nodeName))
				logger.Info("get zk addr: ", zap.Any("union", util.GetZKAddr(client, nodeName)))

				for _, zkPodUnion := range zkPodUnions {
					if strings.Join(zkPodUnion.ZkAddr, "") != "" {
						util.DeletePath(zkPodUnion.ZkAddr, zkPodUnion.PodIP, deleteResponse)
					} else {
						logger.Error("zookeeper address is empty")
					}
				}

				logger.Info("get pod: ", zap.Any("union", zkPodUnions))

				// call apiserver, set taints node, set taints for calico-node
				_, err := util.AddErrorTaintToNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
					client, v1.TaintEffectNoExecute)
				if err != nil {
					logger.Error("set node %s taint error " + nodeName + err.Error())
				}

				// change Switch OFF
				util.ChangeSwitch(isolateclientset, nodeName)

				// sent event
				util.EventRecorder(client, nodeName, util.FormatMessage(healthProbe), "NodeIsolated")
			}
		}
	}
}
