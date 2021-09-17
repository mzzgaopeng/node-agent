# node-agent

k8s node problem auto discovery and isolation

## developer guide

```
mkdir -p $GOPATH/src/harmonycloud.cn/agents
cd $GOPATH/src/harmonycloud.cn/agents
git clone http://10.10.102.104:8000/huan/node-agent.git

```

## how to deploy node-agent

### 1. 创建systemd service文件

```bash
[Unit]
Description=node-agent Service
After=network.target

[Service]
Type=simple
ExecStart=/usr/bin/node-agent --etcdips="10.10.103.182,10.10.103.183,10.10.103.184" --ttltime=3600 --v=4 --kubeconfig=/home/zhutianqi/develop/admin.conf
Restart=always

[Install]
WantedBy=multi-user.target
```

参数含义：

- etcdip：etcd集群ip，以逗号分隔**（注意逗号后面不要加空格）**
- ttltime：默认在多节点故障场景每1小时（3600s）隔离一个节点
- v：指定日志等级，4为info，warning，error，5为debug，info，warning，error
- kubeconfig**（可选）**：非kubernetes集群内部署需要配置kubeconfig文件
- log_dir**（可选）**： 将日志输出到指定目录



