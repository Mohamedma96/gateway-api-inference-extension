/*
Copyright 2025 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package datalayer

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	fwkdl "sigs.k8s.io/gateway-api-inference-extension/pkg/epp/framework/interface/datalayer"
	fwkplugin "sigs.k8s.io/gateway-api-inference-extension/pkg/epp/framework/interface/plugin"
)

// mockTicker implements Ticker for testing
type mockTicker struct {
	ch     chan time.Time
	closed bool
}

func newMockTicker() *mockTicker {
	return &mockTicker{
		ch: make(chan time.Time, 1),
	}
}

func (m *mockTicker) Channel() <-chan time.Time {
	return m.ch
}

func (m *mockTicker) Stop() {
	m.closed = true
}

func (m *mockTicker) Tick() {
	if !m.closed {
		m.ch <- time.Now()
	}
}

// mockPollingDataSource implements PollingDataSource for testing
type mockPollingDataSource struct {
	name      string
	pollCount int
	pollError error
}

func (m *mockPollingDataSource) TypedName() fwkplugin.TypedName {
	return fwkplugin.TypedName{Type: "mock", Name: m.name}
}

func (m *mockPollingDataSource) Poll(ctx context.Context, ep fwkdl.Endpoint) error {
	m.pollCount++
	return m.pollError
}

func (m *mockPollingDataSource) Extractors() []string {
	return []string{}
}

func (m *mockPollingDataSource) OutputType() reflect.Type {
	return nil
}

func (m *mockPollingDataSource) ExtractorType() reflect.Type {
	return nil
}

func (m *mockPollingDataSource) AddExtractor(extractor fwkdl.Extractor) error {
	return nil
}

func TestCollectorMetrics_Disabled(t *testing.T) {
	// Create collector without metrics
	collector := NewCollector()

	if collector.metrics != nil {
		t.Error("Expected metrics to be nil when not enabled")
	}

	metrics := collector.GetMetrics()
	if metrics != nil {
		t.Error("Expected GetMetrics to return nil when metrics disabled")
	}
}

func TestCollectorMetrics_Enabled(t *testing.T) {
	// Create collector with metrics enabled
	collector := NewCollector(WithMetrics())

	if collector.metrics == nil {
		t.Fatal("Expected metrics to be initialized")
	}

	metrics := collector.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected GetMetrics to return non-nil snapshot")
	}

	// Verify initial state
	if metrics.TotalInvocations != 0 {
		t.Errorf("Expected TotalInvocations to be 0, got %d", metrics.TotalInvocations)
	}
	if metrics.TotalErrors != 0 {
		t.Errorf("Expected TotalErrors to be 0, got %d", metrics.TotalErrors)
	}
}

func TestCollectorMetrics_TrackInvocations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create endpoint
	endpoint := fwkdl.NewEndpoint(&fwkdl.EndpointMetadata{
		NamespacedName: types.NamespacedName{Name: "test-pod", Namespace: "default"},
		Address:        "10.0.0.1",
		Port:           "8080",
	}, nil)

	// Create mock source
	mockSource := &mockPollingDataSource{name: "test-source"}

	// Create collector with metrics
	collector := NewCollector(WithMetrics())
	ticker := newMockTicker()

	// Start collector
	err := collector.Start(ctx, ticker, endpoint, []fwkdl.DataSource{mockSource})
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Trigger a few collection cycles
	for i := 0; i < 3; i++ {
		ticker.Tick()
		time.Sleep(10 * time.Millisecond) // Give goroutine time to process
	}

	// Stop collector
	cancel()
	time.Sleep(10 * time.Millisecond)

	// Check metrics
	metrics := collector.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected metrics snapshot")
	}

	if metrics.TotalInvocations != 3 {
		t.Errorf("Expected 3 invocations, got %d", metrics.TotalInvocations)
	}

	if metrics.TotalSourceInvocations != 3 {
		t.Errorf("Expected 3 source invocations, got %d", metrics.TotalSourceInvocations)
	}

	if metrics.TotalErrors != 0 {
		t.Errorf("Expected 0 errors, got %d", metrics.TotalErrors)
	}

	if metrics.LastInvocationTime.IsZero() {
		t.Error("Expected LastInvocationTime to be set")
	}
}

func TestCollectorMetrics_TrackErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create endpoint
	endpoint := fwkdl.NewEndpoint(&fwkdl.EndpointMetadata{
		NamespacedName: types.NamespacedName{Name: "test-pod", Namespace: "default"},
		Address:        "10.0.0.1",
		Port:           "8080",
	}, nil)

	// Create mock source that returns errors
	mockSource := &mockPollingDataSource{
		name:      "error-source",
		pollError: errors.New("test error"),
	}

	// Create collector with metrics
	collector := NewCollector(WithMetrics())
	ticker := newMockTicker()

	// Start collector
	err := collector.Start(ctx, ticker, endpoint, []fwkdl.DataSource{mockSource})
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Trigger collection cycles
	for i := 0; i < 2; i++ {
		ticker.Tick()
		time.Sleep(10 * time.Millisecond)
	}

	// Stop collector
	cancel()
	time.Sleep(10 * time.Millisecond)

	// Check metrics
	metrics := collector.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected metrics snapshot")
	}

	if metrics.TotalInvocations != 2 {
		t.Errorf("Expected 2 invocations, got %d", metrics.TotalInvocations)
	}

	if metrics.TotalErrors != 2 {
		t.Errorf("Expected 2 errors, got %d", metrics.TotalErrors)
	}

	if metrics.TotalSourceErrors != 2 {
		t.Errorf("Expected 2 source errors, got %d", metrics.TotalSourceErrors)
	}

	if metrics.LastErrorTime.IsZero() {
		t.Error("Expected LastErrorTime to be set")
	}
}

func TestCollectorMetrics_MultipleSourcesWithErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create endpoint
	endpoint := fwkdl.NewEndpoint(&fwkdl.EndpointMetadata{
		NamespacedName: types.NamespacedName{Name: "test-pod", Namespace: "default"},
		Address:        "10.0.0.1",
		Port:           "8080",
	}, nil)

	// Create multiple sources, some with errors
	source1 := &mockPollingDataSource{name: "source1"} // no error
	source2 := &mockPollingDataSource{name: "source2", pollError: errors.New("error2")}
	source3 := &mockPollingDataSource{name: "source3"} // no error

	// Create collector with metrics
	collector := NewCollector(WithMetrics())
	ticker := newMockTicker()

	// Start collector
	err := collector.Start(ctx, ticker, endpoint, []fwkdl.DataSource{source1, source2, source3})
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Trigger one collection cycle
	ticker.Tick()
	time.Sleep(10 * time.Millisecond)

	// Stop collector
	cancel()
	time.Sleep(10 * time.Millisecond)

	// Check metrics
	metrics := collector.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected metrics snapshot")
	}

	if metrics.TotalInvocations != 1 {
		t.Errorf("Expected 1 invocation, got %d", metrics.TotalInvocations)
	}

	if metrics.TotalSourceInvocations != 3 {
		t.Errorf("Expected 3 source invocations, got %d", metrics.TotalSourceInvocations)
	}

	if metrics.TotalSourceErrors != 1 {
		t.Errorf("Expected 1 source error, got %d", metrics.TotalSourceErrors)
	}

	if metrics.TotalErrors != 1 {
		t.Errorf("Expected 1 cycle error, got %d", metrics.TotalErrors)
	}
}

func TestEndpointLifecycle_WithCollectorMetrics(t *testing.T) {
	ctx := context.Background()

	// Create a mock polling source
	mockSource := &mockPollingDataSource{
		name:      "mock-source",
		pollError: nil,
	}

	// Register the source
	sources := []fwkdl.DataSource{mockSource}

	// Create factory with metrics enabled
	factory := NewEndpointFactory(sources, 100*time.Millisecond, WithCollectorMetrics())

	if !factory.enableCollectorMetrics {
		t.Error("Expected collector metrics to be enabled")
	}

	// Create an endpoint
	metadata := &fwkdl.EndpointMetadata{
		NamespacedName: types.NamespacedName{Name: "test-pod", Namespace: "default"},
		Address:        "10.0.0.1",
		Port:           "8080",
	}

	endpoint := factory.NewEndpoint(ctx, metadata, nil)
	if endpoint == nil {
		t.Fatal("Expected endpoint to be created")
	}

	// Give collector time to start
	time.Sleep(20 * time.Millisecond)

	// Get collector metrics
	key := types.NamespacedName{Name: "test-pod", Namespace: "default"}
	metrics := factory.GetCollectorMetrics(key)
	if metrics == nil {
		t.Error("Expected collector metrics to be available")
	}

	// Get all metrics
	allMetrics := factory.GetAllCollectorMetrics()
	if len(allMetrics) != 1 {
		t.Errorf("Expected 1 collector with metrics, got %d", len(allMetrics))
	}

	// Cleanup
	factory.ReleaseEndpoint(endpoint)
}

func TestEndpointLifecycle_WithoutCollectorMetrics(t *testing.T) {
	ctx := context.Background()

	// Create factory without metrics
	factory := NewEndpointFactory(nil, 100*time.Millisecond)

	if factory.enableCollectorMetrics {
		t.Error("Expected collector metrics to be disabled")
	}

	// Create an endpoint
	metadata := &fwkdl.EndpointMetadata{
		NamespacedName: types.NamespacedName{Name: "test-pod", Namespace: "default"},
		Address:        "10.0.0.1",
		Port:           "8080",
	}

	endpoint := factory.NewEndpoint(ctx, metadata, nil)
	if endpoint == nil {
		t.Fatal("Expected endpoint to be created")
	}

	// Get collector metrics - should be nil
	key := types.NamespacedName{Name: "test-pod", Namespace: "default"}
	metrics := factory.GetCollectorMetrics(key)
	if metrics != nil {
		t.Error("Expected collector metrics to be nil when disabled")
	}

	// Get all metrics - should be empty
	allMetrics := factory.GetAllCollectorMetrics()
	if len(allMetrics) != 0 {
		t.Errorf("Expected 0 collectors with metrics, got %d", len(allMetrics))
	}

	// Cleanup
	factory.ReleaseEndpoint(endpoint)
}
