package router

import (
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"
)

// ServiceRegistry manages service instances and provides routing capabilities
type ServiceRegistry struct {
	mu        sync.RWMutex
	instances map[string]*ServiceInstance              // instanceID -> instance
	services  map[string]map[string]*ServiceInstance   // serviceName -> instanceID -> instance
	routing   map[string]map[string][]*ServiceInstance // serviceName -> routingKey -> instances
}

// NewServiceRegistry creates a new service registry
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		instances: make(map[string]*ServiceInstance),
		services:  make(map[string]map[string]*ServiceInstance),
		routing:   make(map[string]map[string][]*ServiceInstance),
	}
}

// RegisterInstance registers a new service instance
func (sr *ServiceRegistry) RegisterInstance(instance *ServiceInstance) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Check if instance already exists
	if _, exists := sr.instances[instance.InstanceID]; exists {
		return fmt.Errorf("instance %s already registered", instance.InstanceID)
	}

	// Register the instance
	sr.instances[instance.InstanceID] = instance

	// Register by service name
	if sr.services[instance.ServiceName] == nil {
		sr.services[instance.ServiceName] = make(map[string]*ServiceInstance)
	}
	sr.services[instance.ServiceName][instance.InstanceID] = instance

	// Register by routing key (if provided in metadata)
	fmt.Printf("DEBUG: Registering instance '%s' with metadata: %v\n", instance.InstanceID, instance.Metadata)
	if routingKey, exists := instance.Metadata["routing_key"]; exists {
		fmt.Printf("DEBUG: Found routing_key '%s' for instance '%s'\n", routingKey, instance.InstanceID)
		if sr.routing[instance.ServiceName] == nil {
			sr.routing[instance.ServiceName] = make(map[string][]*ServiceInstance)
		}

		// Check if routing key already exists for this service
		if existingInstances, exists := sr.routing[instance.ServiceName][routingKey]; exists && len(existingInstances) > 0 {
			return status.Errorf(codes.AlreadyExists, "routing key '%s' already exists for service '%s' (instance: %s)",
				routingKey, instance.ServiceName, existingInstances[0].InstanceID)
		}

		// Register as the single instance for this routing key
		sr.routing[instance.ServiceName][routingKey] = []*ServiceInstance{instance}
		fmt.Printf("DEBUG: Successfully registered routing key '%s' for service '%s'\n", routingKey, instance.ServiceName)
	} else {
		fmt.Printf("DEBUG: No routing_key found in metadata for instance '%s'\n", instance.InstanceID)
	}

	return nil
}

// UnregisterInstance removes a service instance
func (sr *ServiceRegistry) UnregisterInstance(instanceID string) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	instance, exists := sr.instances[instanceID]
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	// Remove from instances
	delete(sr.instances, instanceID)

	// Remove from services
	if serviceInstances, exists := sr.services[instance.ServiceName]; exists {
		delete(serviceInstances, instanceID)
		if len(serviceInstances) == 0 {
			delete(sr.services, instance.ServiceName)
		}
	}

	// Remove from routing
	if routingKey, exists := instance.Metadata["routing_key"]; exists {
		if routingMap, exists := sr.routing[instance.ServiceName]; exists {
			if instances, exists := routingMap[routingKey]; exists {
				// Remove instance from slice
				for i, inst := range instances {
					if inst.InstanceID == instanceID {
						routingMap[routingKey] = append(instances[:i], instances[i+1:]...)
						break
					}
				}
				// Clean up empty entries
				if len(routingMap[routingKey]) == 0 {
					delete(routingMap, routingKey)
				}
			}
		}
	}

	// Cancel instance context
	if instance.cancel != nil {
		instance.cancel()
	}

	return nil
}

// GetInstance retrieves a service instance by ID
func (sr *ServiceRegistry) GetInstance(instanceID string) (*ServiceInstance, error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	instance, exists := sr.instances[instanceID]
	if !exists {
		return nil, fmt.Errorf("instance %s not found", instanceID)
	}

	return instance, nil
}

// ListInstances returns all instances for a service, optionally filtered by health status
func (sr *ServiceRegistry) ListInstances(serviceName string, statusFilter pb.HealthStatus) []*ServiceInstance {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	var instances []*ServiceInstance

	if serviceName == "" {
		// Return all instances
		for _, instance := range sr.instances {
			if statusFilter == pb.HealthStatus_UNKNOWN || instance.HealthStatus == statusFilter {
				instances = append(instances, instance)
			}
		}
	} else {
		// Return instances for specific service
		if serviceInstances, exists := sr.services[serviceName]; exists {
			for _, instance := range serviceInstances {
				if statusFilter == pb.HealthStatus_UNKNOWN || instance.HealthStatus == statusFilter {
					instances = append(instances, instance)
				}
			}
		}
	}

	return instances
}

// SelectInstance selects an appropriate instance for routing
func (sr *ServiceRegistry) SelectInstance(serviceName, routingKey string) (*ServiceInstance, error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// Debug logging
	fmt.Printf("DEBUG: SelectInstance called with serviceName='%s', routingKey='%s'\n", serviceName, routingKey)
	if routingMap, exists := sr.routing[serviceName]; exists {
		fmt.Printf("DEBUG: Available routing keys for service '%s': %v\n", serviceName, getKeys(routingMap))
	} else {
		fmt.Printf("DEBUG: No routing map found for service '%s'\n", serviceName)
	}

	// If routing key is provided, find the exact instance with that key
	if routingKey != "" {
		if routingMap, exists := sr.routing[serviceName]; exists {
			if instances, exists := routingMap[routingKey]; exists && len(instances) > 0 {
				instance := instances[0] // Should only be one instance per routing key
				fmt.Printf("DEBUG: Found instance '%s' for routing key '%s', healthy=%t, connected=%t\n",
					instance.InstanceID, routingKey, instance.HealthStatus == pb.HealthStatus_HEALTHY, instance.Connected)
				if instance.HealthStatus == pb.HealthStatus_HEALTHY && instance.Connected {
					return instance, nil
				}
				return nil, status.Errorf(codes.Unavailable, "instance for routing key '%s' is unhealthy", routingKey)
			}
		}
		return nil, status.Errorf(codes.NotFound, "no instance found for routing key '%s'", routingKey)
	}

	// For empty routing key, return NotFound (no fallback load balancing in v1)
	return nil, status.Errorf(codes.InvalidArgument, "routing key is required")
}

// Helper function to get map keys for debugging
func getKeys(m map[string][]*ServiceInstance) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// filterHealthyInstances filters instances by health status
func (sr *ServiceRegistry) filterHealthyInstances(instances []*ServiceInstance) []*ServiceInstance {
	var healthy []*ServiceInstance
	for _, instance := range instances {
		if instance.HealthStatus == pb.HealthStatus_HEALTHY && instance.Connected {
			healthy = append(healthy, instance)
		}
	}
	return healthy
}

// selectByStrategy selects an instance using a load balancing strategy
func (sr *ServiceRegistry) selectByStrategy(instances []*ServiceInstance, routingKey string) *ServiceInstance {
	if len(instances) == 1 {
		return instances[0]
	}

	// Use consistent hashing for routing key
	if routingKey != "" {
		hash := fnv.New32a()
		hash.Write([]byte(routingKey))
		index := hash.Sum32() % uint32(len(instances))
		return instances[index]
	}

	// Simple round-robin based on timestamp
	index := time.Now().UnixNano() % int64(len(instances))
	return instances[index]
}

// UpdateInstanceHealth updates the health status of an instance
func (sr *ServiceRegistry) UpdateInstanceHealth(instanceID string, health pb.HealthStatus) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	instance, exists := sr.instances[instanceID]
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	instance.HealthStatus = health
	instance.LastHeartbeat = time.Now()

	return nil
}

// GetServiceStats returns statistics about registered services
func (sr *ServiceRegistry) GetServiceStats() map[string]ServiceStats {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	stats := make(map[string]ServiceStats)

	for serviceName, instances := range sr.services {
		stat := ServiceStats{
			ServiceName:    serviceName,
			TotalInstances: len(instances),
		}

		for _, instance := range instances {
			switch instance.HealthStatus {
			case pb.HealthStatus_HEALTHY:
				stat.HealthyInstances++
			case pb.HealthStatus_UNHEALTHY:
				stat.UnhealthyInstances++
			case pb.HealthStatus_STARTING:
				stat.StartingInstances++
			case pb.HealthStatus_STOPPING:
				stat.StoppingInstances++
			default:
				stat.UnknownInstances++
			}
		}

		stats[serviceName] = stat
	}

	return stats
}

// ServiceStats holds statistics for a service
type ServiceStats struct {
	ServiceName        string
	TotalInstances     int
	HealthyInstances   int
	UnhealthyInstances int
	StartingInstances  int
	StoppingInstances  int
	UnknownInstances   int
}

// CleanupStaleInstances removes instances that haven't sent heartbeats
func (sr *ServiceRegistry) CleanupStaleInstances(timeout time.Duration) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	now := time.Now()
	var staleInstances []string

	for instanceID, instance := range sr.instances {
		if now.Sub(instance.LastHeartbeat) > timeout {
			staleInstances = append(staleInstances, instanceID)
		}
	}

	// Remove stale instances (unlock to avoid deadlock)
	sr.mu.Unlock()
	for _, instanceID := range staleInstances {
		sr.UnregisterInstance(instanceID)
	}
	sr.mu.Lock()
}
