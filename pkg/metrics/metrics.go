// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "composite_dra"

// Driver: synthesis pipeline
var (
	SynthesisDevicesTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "synthesis_devices_total",
		Help:      "Number of composite devices currently published per composition.",
	}, []string{"composition"})

	SynthesisDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "synthesis_duration_seconds",
		Help:      "Time to recompute and publish composite devices.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"composition"})
)

// Driver: prepare/unprepare
var (
	PrepareDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "prepare_duration_seconds",
		Help:      "End-to-end PrepareResourceClaims time per claim.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"composition"})

	PrepareShadowCreateDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "prepare_shadow_create_duration_seconds",
		Help:      "Time spent creating shadow claims (Phase 1 of Prepare).",
		Buckets:   prometheus.DefBuckets,
	}, []string{"composition"})

	PrepareGRPCDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "prepare_grpc_duration_seconds",
		Help:      "Time spent on gRPC calls to underlying drivers (Phase 2 of Prepare).",
		Buckets:   prometheus.DefBuckets,
	}, []string{"composition"})

	ShadowClaimsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "shadow_claims_active",
		Help:      "Number of active shadow claims.",
	}, []string{"composition"})

	ReconcilerClaimsCleanedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "reconciler_claims_cleaned_total",
		Help:      "Total orphaned shadow claims deleted by the plugin reconciler.",
	})

	GRPCErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "grpc_errors_total",
		Help:      "Total gRPC errors by composition and source driver.",
	}, []string{"composition", "source_driver"})

	DeviceParamsErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "device_params_errors_total",
		Help:      "Total device parameter resolution failures.",
	}, []string{"composition"})
)

// Webhook
var (
	WebhookMutationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "webhook_mutations_total",
		Help:      "Total pods mutated by the webhook.",
	}, []string{"composition"})

	WebhookSkippedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "webhook_skipped_total",
		Help:      "Total pods skipped by the webhook.",
	}, []string{"reason"})

	WebhookErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "webhook_errors_total",
		Help:      "Total webhook errors by stage.",
	}, []string{"stage"})

	WebhookDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "webhook_duration_seconds",
		Help:      "Mutation request latency.",
		Buckets:   prometheus.DefBuckets,
	})

	WebhookTemplatesCreatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "webhook_templates_created_total",
		Help:      "Total ResourceClaimTemplates created.",
	}, []string{"composition"})

	WebhookReconcilerTemplatesCleanedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "webhook_reconciler_templates_cleaned_total",
		Help:      "Total stale ResourceClaimTemplates deleted by the webhook reconciler.",
	})
)
