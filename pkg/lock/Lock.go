package lock

import (
	"context"
	"fmt"
	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/pkg/transport"
	"go.uber.org/zap"
	"time"
)

type EtcdMutex struct {
	Ttl     int64              //租约时间
	Conf    clientv3.Config    //etcd集群配置
	Key     string             //etcd的key
	cancel  context.CancelFunc //关闭续租的func
	lease   clientv3.Lease
	leaseID clientv3.LeaseID
	txn     clientv3.Txn
}

func (em *EtcdMutex) init() error {
	var err error
	//var ctx context.Context
	client, err := clientv3.New(em.Conf)
	if err != nil {
		return err
	}
	em.txn = clientv3.NewKV(client).Txn(context.TODO())
	em.lease = clientv3.NewLease(client)
	leaseResp, err := em.lease.Grant(context.TODO(), em.Ttl)
	if err != nil {
		return err
	}
	//ctx, em.cancel = context.WithCancel(context.TODO())
	_, em.cancel = context.WithCancel(context.TODO())
	em.leaseID = leaseResp.ID
	//_,err = em.lease.KeepAlive(ctx,em.leaseID)

	return err
}
func (em *EtcdMutex) Lock(nodename string) (error, string) {
	err := em.init()
	value := ""
	if err != nil {
		return err, value
	}
	//LOCK:
	newrevision := clientv3.CreateRevision(em.Key)
	em.txn.If(clientv3.Compare(newrevision, "=", 0)).
		Then(clientv3.OpPut(em.Key, nodename, clientv3.WithLease(em.leaseID))).
		Else(clientv3.OpGet(em.Key))

	//em.txn.If( clientv3.Compare(newrevision, "=", 3)).
	//	Then(clientv3.OpPut(em.Key, nodename, clientv3.WithLease(em.leaseID))).
	//	Else()
	txnResp, err := em.txn.Commit()
	if err != nil {
		return err, value
	}
	if !txnResp.Succeeded { //判断txn.if条件是否成立
		for _, v := range txnResp.Responses {
			for _, kv := range v.GetResponseRange().Kvs {
				if string(kv.Key) == "NodeIsolated" {
					value = string(kv.Value)
					break
				}
			}
			if value != "" {
				break
			}
		}
		return fmt.Errorf("Locking failure"), value
	}
	return nil, value
}
func (em *EtcdMutex) UnLock() {
	logger := zap.L()

	em.cancel()
	_, err := em.lease.Revoke(context.TODO(), em.leaseID)
	if err != nil {
		logger.Error(err.Error())
	}
	logger.Info("Release lock")
}

func maketlsconfig(certfile string, cafile string, keyfile string) transport.TLSInfo {
	tlsinfo := transport.TLSInfo{}
	tlsinfo.CertFile = certfile
	tlsinfo.CAFile = cafile
	tlsinfo.KeyFile = keyfile

	return tlsinfo
}
func MakeEtcdMutex(endpoints []string, ttl int64, certfile string, cafile string, keyfile string) *EtcdMutex {
	tlsinfo := maketlsconfig(certfile, cafile, keyfile)
	tls, err := tlsinfo.ClientConfig()
	if err != nil {
		fmt.Println(err)
	}
	var conf = clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
		TLS:         tls,
	}
	eMutex := &EtcdMutex{
		Conf: conf,
		Ttl:  ttl, //3600,
		Key:  "NodeIsolated",
	}

	return eMutex
}
