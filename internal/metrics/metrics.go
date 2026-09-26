// Package metrics provides Prometheus metrics for debian-repo
package metrics

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the service
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal   prometheus.CounterVec
	HTTPRequestDuration prometheus.HistogramVec

	// Registration metrics
	RegistrationsTotal        prometheus.CounterVec
	RegistrationErrorsTotal   prometheus.CounterVec
	RegistrationDuration      prometheus.HistogramVec
	RegistrationValidationErr prometheus.CounterVec

	// Index state metrics
	IndexDistributions prometheus.GaugeFunc
	IndexPackages      prometheus.GaugeFunc
	IndexArchitectures prometheus.GaugeFunc
	SnapshotGeneration prometheus.Gauge

	// Pool serving metrics
	PoolDownloadsTotal prometheus.CounterVec
	PoolBytesTotal     prometheus.CounterVec

	// Storage metrics
	MinIOOperationsTotal prometheus.CounterVec
	MinIOErrorsTotal     prometheus.CounterVec
	MinIODuration        prometheus.HistogramVec

	// GPG metrics
	GPGSigningsTotal   prometheus.CounterVec
	GPGSigningErrors   prometheus.CounterVec
	GPGSigningDuration prometheus.HistogramVec

	mu       sync.RWMutex
	indexMgr IndexManager
}

// IndexManager is the interface for accessing index state
type IndexManager interface {
	ListDistributions() []string
	CountPackages(suite string) int
	ListArchitectures(suite string) []string
	SnapshotGen() uint64
}

var instance *Metrics
var once sync.Once

// Init initializes the global metrics singleton with the given index manager
func Init(indexMgr IndexManager) *Metrics {
	once.Do(func() {
		instance = &Metrics{
			indexMgr: indexMgr,
			HTTPRequestsTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_http_requests_total",
				Help: "Total HTTP requests by method and path",
			}, []string{"method", "path", "status"}),
			HTTPRequestDuration: *promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "debian_repo_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			}, []string{"method", "path"}),
			RegistrationsTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_registrations_total",
				Help: "Total package registrations by suite and component",
			}, []string{"suite", "component"}),
			RegistrationErrorsTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_registration_errors_total",
				Help: "Total registration errors by reason",
			}, []string{"reason"}),
			RegistrationDuration: *promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "debian_repo_registration_duration_seconds",
				Help:    "Package registration duration in seconds",
				Buckets: prometheus.DefBuckets,
			}, []string{"suite", "component"}),
			RegistrationValidationErr: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_registration_validation_errors_total",
				Help: "Total validation errors during registration",
			}, []string{"error_type"}),
			SnapshotGeneration: promauto.NewGauge(prometheus.GaugeOpts{
				Name: "debian_repo_snapshot_generation",
				Help: "Current snapshot generation number",
			}),
			PoolDownloadsTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_pool_downloads_total",
				Help: "Total pool downloads by suite and component",
			}, []string{"suite", "component", "architecture"}),
			PoolBytesTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_pool_bytes_total",
				Help: "Total bytes downloaded from pool",
			}, []string{"suite", "component"}),
			MinIOOperationsTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_minio_operations_total",
				Help: "Total MinIO operations by type",
			}, []string{"operation"}),
			MinIOErrorsTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_minio_errors_total",
				Help: "Total MinIO errors by operation",
			}, []string{"operation"}),
			MinIODuration: *promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "debian_repo_minio_duration_seconds",
				Help:    "MinIO operation duration in seconds",
				Buckets: prometheus.DefBuckets,
			}, []string{"operation"}),
			GPGSigningsTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_gpg_signings_total",
				Help: "Total GPG signing operations",
			}, []string{"key_id"}),
			GPGSigningErrors: *promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "debian_repo_gpg_signing_errors_total",
				Help: "Total GPG signing errors",
			}, []string{"key_id"}),
			GPGSigningDuration: *promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "debian_repo_gpg_signing_duration_seconds",
				Help:    "GPG signing operation duration in seconds",
				Buckets: prometheus.DefBuckets,
			}, []string{"key_id"}),
		}

		// Register gauge functions for dynamic index state
		promauto.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "debian_repo_distributions_count",
			Help: "Number of distributions in the index",
		}, func() float64 {
			instance.mu.RLock()
			defer instance.mu.RUnlock()
			if instance.indexMgr == nil {
				return 0
			}
			return float64(len(instance.indexMgr.ListDistributions()))
		})

		promauto.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "debian_repo_packages_total",
			Help: "Total number of packages across all distributions",
		}, func() float64 {
			instance.mu.RLock()
			defer instance.mu.RUnlock()
			if instance.indexMgr == nil {
				return 0
			}
			total := 0
			for _, suite := range instance.indexMgr.ListDistributions() {
				total += instance.indexMgr.CountPackages(suite)
			}
			return float64(total)
		})
	})
	return instance
}

// Get returns the global metrics singleton
func Get() *Metrics {
	return instance
}

// RecordHTTPRequest records an HTTP request
func (m *Metrics) RecordHTTPRequest(method, path string, status int, duration float64) {
	if m == nil {
		return
	}
	statusStr := fmt.Sprintf("%d", status)
	m.HTTPRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration)
}

// RecordRegistration records a successful registration
func (m *Metrics) RecordRegistration(suite, component string, duration float64) {
	if m == nil {
		return
	}
	m.RegistrationsTotal.WithLabelValues(suite, component).Inc()
	m.RegistrationDuration.WithLabelValues(suite, component).Observe(duration)
}

// RecordRegistrationError records a registration error
func (m *Metrics) RecordRegistrationError(reason string) {
	if m == nil {
		return
	}
	m.RegistrationErrorsTotal.WithLabelValues(reason).Inc()
}

// RecordValidationError records a validation error
func (m *Metrics) RecordValidationError(errorType string) {
	if m == nil {
		return
	}
	m.RegistrationValidationErr.WithLabelValues(errorType).Inc()
}

// RecordPoolDownload records a pool file download
func (m *Metrics) RecordPoolDownload(suite, component, architecture string, bytes int64) {
	if m == nil {
		return
	}
	m.PoolDownloadsTotal.WithLabelValues(suite, component, architecture).Inc()
	m.PoolBytesTotal.WithLabelValues(suite, component).Add(float64(bytes))
}

// RecordMinIOOperation records a MinIO operation
func (m *Metrics) RecordMinIOOperation(operation string, duration float64, err error) {
	if m == nil {
		return
	}
	m.MinIOOperationsTotal.WithLabelValues(operation).Inc()
	m.MinIODuration.WithLabelValues(operation).Observe(duration)
	if err != nil {
		m.MinIOErrorsTotal.WithLabelValues(operation).Inc()
	}
}

// RecordGPGSigning records a GPG signing operation
func (m *Metrics) RecordGPGSigning(keyID string, duration float64, err error) {
	if m == nil {
		return
	}
	m.GPGSigningsTotal.WithLabelValues(keyID).Inc()
	m.GPGSigningDuration.WithLabelValues(keyID).Observe(duration)
	if err != nil {
		m.GPGSigningErrors.WithLabelValues(keyID).Inc()
	}
}

// UpdateSnapshotGeneration updates the snapshot generation gauge
func (m *Metrics) UpdateSnapshotGeneration(gen uint64) {
	if m == nil {
		return
	}
	m.SnapshotGeneration.Set(float64(gen))
}
