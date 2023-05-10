package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/helper"
)

const deviceAppPluginName = "device-app-plugin"
const preFilterStateKey = "PreFilter-bandwidth"

type DeviceAppPlugin struct {
	// define any necessary fields for your plugin
	handle framework.Handle
}

var _ framework.PreFilterPlugin = &DeviceAppPlugin{}
var _ framework.FilterPlugin = &DeviceAppPlugin{}
var _ framework.ScorePlugin = &DeviceAppPlugin{}

type preFilterState struct {
	framework.NodeInfo
	predictUrl string
}

type networkTraffic struct {
	latencyMS   float32
	concurrency float32
}

func (s *preFilterState) Clone() framework.StateData {
	return s
}

func newDevicePlugin(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	// initialize your plugin with the given FrameworkHandle
	return &DeviceAppPlugin{handle: handle}, nil
}

func (p *DeviceAppPlugin) Name() string {
	return deviceAppPluginName
}

func (p *DeviceAppPlugin) PreFilter(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod) *framework.Status {
	annotations := pod.Annotations
	if dataSourceNode, found := annotations["dataSourceNode"]; found {
		// get dataSourceNode from pod annotations
		sNodeInfo, err := p.handle.SnapshotSharedLister().NodeInfos().Get(dataSourceNode)
		if err != nil {
			return framework.AsStatus(fmt.Errorf("getting node %q from Snapshot: %w", dataSourceNode, err))
		}
		predictUrl := "http://127.0.0.1:12345/predict"
		url, ok := os.LookupEnv("PredictURL")
		if ok {
			predictUrl = url
		}
		p := &preFilterState{
			*sNodeInfo, predictUrl,
		}
		cycleState.Write(preFilterStateKey, p)
		return nil
	}
	return framework.AsStatus(fmt.Errorf("no dataSourceNode annotations on pod"))
}

func (p *DeviceAppPlugin) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

func getPreFilterState(cycleState *framework.CycleState) (*preFilterState, error) {
	c, err := cycleState.Read(preFilterStateKey)
	if err != nil {
		// preFilterState doesn't exist, likely PreFilter wasn't invoked.
		return nil, fmt.Errorf("error reading %q from cycleState: %w", preFilterStateKey, err)
	}

	s, ok := c.(*preFilterState)
	if !ok {
		return nil, fmt.Errorf("%+v  convert to NodeResourcesFit.preFilterState error", c)
	}
	return s, nil
}

func (p *DeviceAppPlugin) Filter(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	// check if node can access device node
	return nil
}

func (p *DeviceAppPlugin) Score(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	// implement your plugin's scoring logic
	s, err := getPreFilterState(cycleState)
	if err != nil {
		return 0, framework.AsStatus(fmt.Errorf("getting preFilterState from cycleState: %w", err))
	}
	dNodeInfo, err := p.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.AsStatus(fmt.Errorf("getting node %q from Snapshot: %w", nodeName, err))
	}
	return p.score(pod, s, dNodeInfo)
}

func (p *DeviceAppPlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	return helper.DefaultNormalizeScore(framework.MaxNodeScore, false, scores)
}

func (p *DeviceAppPlugin) ScoreExtensions() framework.ScoreExtensions {
	return p
}

func (p *DeviceAppPlugin) score(pod *v1.Pod, s *preFilterState, dnode *framework.NodeInfo) (int64, *framework.Status) {
	// use information of snode and dnode to score
	snode := s.NodeInfo
	sCPUUsage := float32(snode.Requested.MilliCPU) / float32(snode.Allocatable.MilliCPU)
	dCPUUsage := float32(dnode.Requested.MilliCPU) / float32(dnode.Allocatable.MilliCPU)

	sMemUsage := float32(snode.Requested.Memory) / float32(snode.Allocatable.Memory)
	dMemUsage := float32(dnode.Requested.Memory) / float32(dnode.Allocatable.Memory)

	predictUrl := s.predictUrl
	client := &http.Client{}

	payload := struct {
		SCPUUsage float32 `json:"s_cpu_usage"`
		SMemUsage float32 `json:"s_mem_usage"`

		DCPUUsage float32 `json:"d_cpu_usage"`
		DMemUsage float32 `json:"d_mem_usage"`
	}{
		SCPUUsage: sCPUUsage,
		SMemUsage: sMemUsage,

		DCPUUsage: dCPUUsage,
		DMemUsage: dMemUsage,
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return -1, nil
	}

	// make a GET request
	req, err := http.NewRequest("POST", predictUrl, bytes.NewBuffer(jsonPayload))
	if err != nil {
		fmt.Println("Error creating request:", err)
		return -1, nil
	}
	// add headers to the request
	req.Header.Set("Content-Type", "application/json")
	// send the request and get the response
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return -1, nil
	}

	// read the response body
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return -1, nil
	}

	var nt networkTraffic
	err = json.Unmarshal(body, &nt)
	if err != nil {
		fmt.Println("Error unmarshaling JSON data:", err)
		return -1, nil
	}
	concurrency := int64(nt.concurrency * 1000)
	return concurrency, nil
}
