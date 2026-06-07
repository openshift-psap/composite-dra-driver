package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/webhook"
)

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

	flag.IntVar(&port, "port", 8443, "webhook server port")
	flag.StringVar(&tlsCert, "tls-cert", "/etc/webhook/certs/tls.crt", "TLS certificate path")
	flag.StringVar(&tlsKey, "tls-key", "/etc/webhook/certs/tls.key", "TLS key path")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig (optional)")
	flag.StringVar(&deviceClass, "device-class", "composite-gpu-nic", "composite DeviceClass name")
	flag.StringVar(&resourceName, "resource-name", "composite.dra/gpu-nic-pair", "synthetic resource name to intercept")
	flag.Parse()

	restConfig, err := buildRESTConfig(kubeconfig)
	if err != nil {
		klog.Fatalf("build REST config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		klog.Fatalf("create kube client: %v", err)
	}

	mutator := webhook.NewMutator(webhook.MutatorConfig{
		DeviceClassName: deviceClass,
		ResourceName:    resourceName,
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
		klog.Infof("webhook starting on :%d (deviceClass=%s, resourceName=%s)", port, deviceClass, resourceName)
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
