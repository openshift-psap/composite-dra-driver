package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/webhook"
)

type stringMapFlag map[string]string

func (f *stringMapFlag) String() string {
	var parts []string
	for k, v := range *f {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func (f *stringMapFlag) Set(val string) error {
	parts := strings.SplitN(val, "=", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid mapping %q, expected resourceName=deviceClassName", val)
	}
	(*f)[parts[0]] = parts[1]
	return nil
}

func main() {
	klog.InitFlags(nil)

	var (
		port           int
		tlsCert        string
		tlsKey         string
		kubeconfig     string
		deviceClass    string
		resourceName   string
	)

	resourceMappings := make(stringMapFlag)

	flag.IntVar(&port, "port", 8443, "webhook server port")
	flag.StringVar(&tlsCert, "tls-cert", "/etc/webhook/certs/tls.crt", "TLS certificate path")
	flag.StringVar(&tlsKey, "tls-key", "/etc/webhook/certs/tls.key", "TLS key path")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig (optional)")
	flag.StringVar(&deviceClass, "device-class", "", "composite DeviceClass name (deprecated: use --resource-mapping)")
	flag.StringVar(&resourceName, "resource-name", "", "synthetic resource name to intercept (deprecated: use --resource-mapping)")
	flag.Var(&resourceMappings, "resource-mapping", "resourceName=deviceClassName mapping (repeatable)")
	flag.Parse()

	// Backward compatibility: if old flags set and no --resource-mapping, build mapping from them
	if len(resourceMappings) == 0 {
		if deviceClass == "" {
			deviceClass = "composite-gpu-nic"
		}
		if resourceName == "" {
			resourceName = "composite.dra/gpu-nic-pair"
		}
		if deviceClass != "" || resourceName != "" {
			klog.Warning("--device-class and --resource-name are deprecated, use --resource-mapping instead")
		}
		resourceMappings[resourceName] = deviceClass
	}

	restConfig, err := buildRESTConfig(kubeconfig)
	if err != nil {
		klog.Fatalf("build REST config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		klog.Fatalf("create kube client: %v", err)
	}

	mutator := webhook.NewMutator(webhook.MutatorConfig{
		ResourceMappings: map[string]string(resourceMappings),
	}, kubeClient.ResourceV1())

	handler := webhook.NewHandler(mutator)

	mux := http.NewServeMux()
	mux.Handle("/mutate", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
	if err != nil {
		klog.Fatalf("load TLS certs: %v", err)
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	go func() {
		klog.Infof("webhook starting on :%d (mappings=%v)", port, resourceMappings)
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			klog.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	klog.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)
}

func buildRESTConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}
