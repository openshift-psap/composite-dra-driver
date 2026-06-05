package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/config"
	"github.com/openshift-psap/composite-dra-driver/pkg/plugin"
	"github.com/openshift-psap/composite-dra-driver/pkg/shadow"
	"github.com/openshift-psap/composite-dra-driver/pkg/store"
	"github.com/openshift-psap/composite-dra-driver/pkg/synthesizer"
)

func main() {
	klog.InitFlags(nil)

	var (
		configPath string
		nodeName   string
		kubeconfig string
		pluginDir  string
		stateDir   string
	)

	flag.StringVar(&configPath, "config", "/etc/composite-dra/config.yaml", "path to driver config")
	flag.StringVar(&nodeName, "node-name", os.Getenv("NODE_NAME"), "node name (defaults to NODE_NAME env)")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig (optional, uses in-cluster if empty)")
	flag.StringVar(&pluginDir, "plugin-dir", "/var/lib/kubelet/plugins", "kubelet plugins directory")
	flag.StringVar(&stateDir, "state-dir", "/var/lib/composite-dra", "directory for persistent state")
	flag.Parse()

	if nodeName == "" {
		klog.Fatal("node name required: set --node-name or NODE_NAME env")
	}

	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		klog.Fatalf("load config: %v", err)
	}
	if err := config.Validate(cfg); err != nil {
		klog.Fatalf("invalid config: %v", err)
	}

	restConfig, err := buildRESTConfig(kubeconfig)
	if err != nil {
		klog.Fatalf("build REST config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		klog.Fatalf("create kube client: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := os.MkdirAll(stateDir, 0755); err != nil {
		klog.Fatalf("create state dir: %v", err)
	}

	stateStore, err := store.NewStateStore(filepath.Join(stateDir, "state.db"))
	if err != nil {
		klog.Fatalf("open state store: %v", err)
	}
	defer stateStore.Close()

	deviceStore := store.NewDeviceStore()

	claimMgr := shadow.NewClaimManager(kubeClient.ResourceV1(), cfg.Driver.Name)
	railResolver := shadow.NewRailConfigResolver(cfg.RailConfig)
	grpcClient := plugin.NewGRPCClient(pluginDir)
	defer grpcClient.Close()

	compositePlugin := plugin.NewCompositePlugin(
		cfg.Driver.Name,
		deviceStore,
		claimMgr,
		railResolver,
		grpcClient,
		stateStore,
	)

	pluginSocketDir := filepath.Join(pluginDir, cfg.Driver.Name)
	if err := os.MkdirAll(pluginSocketDir, 0755); err != nil {
		klog.Fatalf("create plugin socket dir %s: %v", pluginSocketDir, err)
	}

	klog.Infof("composite-dra-driver starting on node %s with driver %s", nodeName, cfg.Driver.Name)

	helper, err := kubeletplugin.Start(ctx, compositePlugin,
		kubeletplugin.DriverName(cfg.Driver.Name),
		kubeletplugin.NodeName(nodeName),
		kubeletplugin.KubeClient(kubeClient),
	)
	if err != nil {
		klog.Fatalf("start kubelet plugin: %v", err)
	}

	publisher := synthesizer.NewHelperPublisher(func(resources resourceslice.DriverResources) error {
		return helper.PublishResources(ctx, resources)
	})

	synth := synthesizer.New(cfg, nodeName, kubeClient, deviceStore, publisher)
	go func() {
		if err := synth.Start(ctx); err != nil {
			klog.Fatalf("synthesizer: %v", err)
		}
	}()

	go plugin.StartReconciler(ctx, kubeClient.ResourceV1(), cfg.Driver.Name, 5*time.Minute)

	<-ctx.Done()
	klog.Info("shutting down")
}

func buildRESTConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}
