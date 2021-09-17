package util

import (
	ZKP "github.com/samuel/go-zookeeper/zk"
	"go.uber.org/zap"
	"regexp"
	"strings"
	"sync"

	// un "github.com/tobyhede/go-underscore"
	"time"
)

func DeletePath(zkAddress []string, deletePodAddress []string, deleteResponse chan string) {
	var (
		path          = "/dubbo"
		result        = []string{}
		deletePath    = []string{}
		svc_wg        = sync.WaitGroup{}
		delete_wg     = sync.WaitGroup{}
		errorResponse = make(chan string)
		conns         = []*ZKP.Conn{}
		count         = []int{0}
		logger        = zap.L()
	)

	zk, _, err := ZKP.Connect(zkAddress, time.Second*15)
	if err != nil {
		logger.Error("[Zookeeper] Connect" + strings.Join(zkAddress, ", ") + " returned error: " + err.Error())
		return
	}
	conns = append(conns, zk)

	leafParentNode, _, err := conns[0].Children(path)
	if err != nil {
		logger.Error("[Zookeeper] Get returned error: " + err.Error())
	}
	defer conns[0].Close()

	for index, element := range leafParentNode {
		leafParentNode[index] = "/dubbo/" + element + "/providers"
	}

	getAllPathTime := time.Now()

	for _, l := range leafParentNode {
		svc_wg.Add(1)
		go findLeafPath(conns[0], l, &result, deletePodAddress, &deletePath, &svc_wg)
	}
	svc_wg.Wait()
	logger.Info("[Zookeeper] get all path: "+time.Since(getAllPathTime).String()+" count: ",
		zap.Int("count", len(result)))
	deleteTime := time.Now()

	go replayError(errorResponse)
	for _, finalPath := range deletePath {
		delete_wg.Add(1)
		go deleteAsync(conns[0], finalPath, -1, &delete_wg, errorResponse, deleteResponse, &count)
		logger.Error("delete zk final path: " + finalPath)
	}

	delete_wg.Wait()

	if count[0] == 0 {
		logger.Info("[Zookeeper] delete all path: "+time.Since(deleteTime).String()+" count: ",
			zap.Int("count", len(deletePath)))

	} else {
		logger.Error("[Zookeeper] delete all path: " + time.Since(deleteTime).String() + " count: " +
			string(len(deletePath)) + "fail: " + string(count[0]))
	}
}

func findLeafPath(conn *ZKP.Conn, leafParentNode string, result *[]string, podIp []string, deletePath *[]string, wg *sync.WaitGroup) {
	defer wg.Done()
	var podIpOk []string
	for _, pIp := range podIp {
		if pIp != "" {
			podIpOk = append(podIpOk, pIp)
		}
	}
	data, _, err := conn.Children(leafParentNode)
	logger := zap.L()

	if err != nil {
		logger.Error("[Zookeeper] " + conn.Server() + "Get returned error: " + err.Error())
	}
	for _, element := range data {
		finalPath := leafParentNode + "/" + element
		// TODO no catch error
		if isContain, _ := regexp.MatchString(strings.Join(podIpOk, "|"), finalPath); isContain {
			// fmt.Println("deletePath: ", finalPath)
			*deletePath = append(*deletePath, finalPath)
		}
		*result = append(*result, leafParentNode+"/"+element)
		// fmt.Println(leafParentNode + "/" + element)
	}
}

func deleteAsync(conn *ZKP.Conn, path string, version int32, wg *sync.WaitGroup,
	errorResponse chan string, deleteResponse chan string, count *[]int) {
	defer wg.Done()

	retries := 3

	for retries > 0 {
		err := conn.Delete(path, version)
		// fmt.Println("delete path: ", path)
		if err != nil {
			// fmt.Println("unknown delete path: ", path, " in zookeeper: ", conn.Server())
			retries = retries - 1
			time.Sleep(time.Duration(1) * time.Second)
			errorResponse <- err.Error()
		} else {
			break
		}
	}

	if retries == 0 {
		(*count)[0] = (*count)[0] + 1
		deleteResponse <- "delete path error: " + path
	}
}

func replayError(errorResponse chan string) {
	logger := zap.L()

	for {
		c := <-errorResponse
		logger.Error("delete error: " + c)
	}
}
