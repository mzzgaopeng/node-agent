package util

import (
	. "github.com/smartystreets/goconvey/convey"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestSpec(t *testing.T) {
	Convey("test for FormatMessage", t, func() {
		dockerChannel := make(chan bool)
		kubeletChannel := make(chan bool)
		containerdChannel := make(chan bool)
		calicoChannel := make(chan bool)
		healthProbe := map[string]Probe{
			"docker":     {"docker", "unix", "/var/run/docker.sock", 1, 3, dockerChannel, true},
			"containerd": {"containerd", "containerd", "/var/run/docker/libcontainerd/docker-containerd.sock", 1, 3, containerdChannel, true},
			"kubelet":    {"kubelet", "http", "http://localhost:10248/healthz", 1, 3, kubeletChannel, true},
			"calico":     {"calico", "http", "http://localhost:9099/liveness", 1, 3, calicoChannel, true},
		}

		Convey("docker down", func() {
			dst := healthProbe
			dst["docker"] = Probe{"docker", "unix", "/var/run/docker.sock", 1, 3, dockerChannel, false}

			So(FormatMessage(dst), ShouldEqual, "docker down")
		})

		Convey("calico down", func() {
			dst := healthProbe
			dst["calico"] = Probe{"calico", "http", "http://localhost:9099/liveness", 1, 3, calicoChannel, false}

			So(FormatMessage(dst), ShouldEqual, "calico down")
		})

		Convey("kubelet down, calico down", func() {
			dst := healthProbe
			dst["kubelet"] = Probe{"kubelet", "http", "http://localhost:10248/healthz", 1, 3, kubeletChannel, false}
			dst["calico"] = Probe{"calico", "http", "http://localhost:9099/liveness", 1, 3, calicoChannel, false}

			So(FormatMessage(dst), ShouldEqual, "kubelet down, calico down")
		})

		Convey("containerd down, calico down", func() {
			dst := healthProbe
			dst["containerd"] = Probe{"containerd", "containerd", "/var/run/docker/libcontainerd/docker-containerd.sock", 1, 3, containerdChannel, false}
			dst["calico"] = Probe{"calico", "http", "http://localhost:9099/liveness", 1, 3, calicoChannel, false}

			So(FormatMessage(dst), ShouldEqual, "containerd down, calico down")
		})
	})

	// test for CheckNodeTaint
	Convey("test for CheckNodeTaint", t, func() {
		Convey("node has taint", func() {
			node := &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "k8s-node4",
				},
				Spec: apiv1.NodeSpec{
					Taints: []apiv1.Taint{
						apiv1.Taint{Key: "harmonycloud.cn/ErrorNode"},
					},
				},
			}

			So(CheckNodeTaint(node), ShouldEqual, true)
		})

		Convey("node has not taint", func() {
			node := &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "k8s-node4",
				},
				Spec: apiv1.NodeSpec{
					Taints: []apiv1.Taint{},
				},
			}

			So(CheckNodeTaint(node), ShouldEqual, false)
		})
	})

	// test for GetZkAddress
	Convey("test for GetZkAddress", t, func() {
		Convey("get pod zk address", func() {
			pod := &apiv1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dubbo pod",
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						apiv1.Container{Env: []apiv1.EnvVar{
							apiv1.EnvVar{Name: "DUBBO_ZK", Value: "zookeeper://10.1.11.239"},
						}},
					},
				},
			}

			So(GetZkAddress(pod), ShouldResemble, []string{"10.1.11.239"})
		})
	})
}
