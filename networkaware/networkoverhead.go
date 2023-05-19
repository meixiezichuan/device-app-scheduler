package networkaware

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"os"

	ctv1 "github.com/meixiezichuan/clustertopology/api/edge/v1"
	ctclient "github.com/meixiezichuan/clustertopology/generated/clientset/versioned"
	ctinformers "github.com/meixiezichuan/clustertopology/generated/informers/externalversions"
	ctlisters "github.com/meixiezichuan/clustertopology/generated/listers/edge/v1"
)

const (
	NetworkOverheadName = "NetworkOverhead"
	preFilterStateKey   = "PreFilter-overhead"
	NetworkTopologyKey  = "topology.kubernetes.io/network"
)

type NetworkOverhead struct {
	// define any necessary fields for your plugin
	handle   framework.Handle
	ctLister ctlisters.ClusterTopologyLister
	ctName   string
}

var _ framework.PreFilterPlugin = &NetworkOverhead{}
var _ framework.FilterPlugin = &NetworkOverhead{}
var _ framework.ScorePlugin = &NetworkOverhead{}

type preFilterState struct {
	snodeInfo           *framework.NodeInfo
	networkOverheadList ctv1.CostList
}

type networkTraffic struct {
	latencyMS   float32
	concurrency float32
}

func (s *preFilterState) Clone() framework.StateData {
	return s
}

func NewNetworkOverhead(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	// initialize your plugin with the given FrameworkHandle
	ctLister, err := InitClusterTopologyInformer(handle.KubeConfig())
	if err != nil {
		return nil, err
	}
	clusterName := "edge1"
	name, ok := os.LookupEnv("CLUSTERNAME")
	if ok {
		clusterName = name
	}
	return &NetworkOverhead{handle: handle, ctLister: ctLister, ctName: clusterName}, nil
}

func (p *NetworkOverhead) Name() string {
	return NetworkOverheadName
}

func (p *NetworkOverhead) PreFilter(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod) *framework.Status {
	annotations := pod.Annotations
	if dataSourceNode, found := annotations["dataSourceNode"]; found {
		// get dataSourceNode from pod annotations
		sNodeInfo, err := p.handle.SnapshotSharedLister().NodeInfos().Get(dataSourceNode)
		if err != nil {
			return framework.AsStatus(fmt.Errorf("getting node %q from Snapshot: %w", dataSourceNode, err))
		}
		ct := p.findNetworkOverheadByNode(dataSourceNode)
		if ct == nil {
			return framework.AsStatus(fmt.Errorf("can not get cluster topology"))
		}
		pre := &preFilterState{
			snodeInfo:           sNodeInfo,
			networkOverheadList: ct,
		}
		cycleState.Write(preFilterStateKey, pre)
		return nil
	}
	return framework.AsStatus(fmt.Errorf("no dataSourceNode annotations on pod"))
}

func (p *NetworkOverhead) findNetworkOverheadByNode(node string) ctv1.CostList {
	clusterTopology, err := p.ctLister.ClusterTopologies("kube-system").Get(p.ctName)
	if err != nil {
		klog.V(4).InfoS("Cannot get networkTopology from networkTopologyNamespaceLister:", "error", err)
		return nil
	}

	tps := clusterTopology.Spec.Topologys
	for _, tp := range tps {
		if tp.TopologyKey == NetworkTopologyKey {
			ol := tp.OriginList
			for _, o := range ol {
				if o.Origin == node {
					return o.CostList
				}
			}
		}
	}
	return nil
}

func (p *NetworkOverhead) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

func InitClusterTopologyInformer(kubeConfig *restclient.Config) (ctlisters.ClusterTopologyLister, error) {
	ctClient, err := ctclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Errorf("Cannot create clientset for Clustertopology Informer: %s, %s", kubeConfig, err)
		return nil, err
	}

	ctInformerFactory := ctinformers.NewSharedInformerFactory(ctClient, 0)
	ctInformer := ctInformerFactory.Edge().V1().ClusterTopologies()
	ctLister := ctInformer.Lister()

	klog.V(5).InfoS("start Clustertopology Informer")
	ctx := context.Background()
	ctInformerFactory.Start(ctx.Done())
	ctInformerFactory.WaitForCacheSync(ctx.Done())

	return ctLister, nil
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

func (p *NetworkOverhead) Filter(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	// check if node can access device node
	return nil
}

func (p *NetworkOverhead) Score(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	// implement your plugin's scoring logic
	s, err := getPreFilterState(cycleState)
	if err != nil {
		return 0, framework.AsStatus(fmt.Errorf("getting preFilterState from cycleState: %w", err))
	}
	return p.score(pod, s, nodeName)
}

func (p *NetworkOverhead) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	var maxCount int64 = -100000
	var minCount int64 = 0
	for i := range scores {
		if scores[i].Score > maxCount {
			maxCount = scores[i].Score
		}
		if scores[i].Score < minCount {
			minCount = scores[i].Score
		}
	}
	for i := range scores {
		score := scores[i].Score

		score = (score - minCount) * 100 / (maxCount - minCount)
		scores[i].Score = score
	}
	return nil
}

func (p *NetworkOverhead) ScoreExtensions() framework.ScoreExtensions {
	return p
}

func (p *NetworkOverhead) score(pod *v1.Pod, s *preFilterState, dnode string) (int64, *framework.Status) {
	// use information of snode and dnode to score
	costList := s.networkOverheadList
	for _, cost := range costList {
		if cost.Destination == dnode {
			return -cost.NetworkCost, nil
		}
	}

	return 0, nil
}
