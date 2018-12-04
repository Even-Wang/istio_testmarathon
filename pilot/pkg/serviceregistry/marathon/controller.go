// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package marathon

import (
	"time"
	"crypto/tls"
	"net"
	"net/http"
	"github.com/gambol99/go-marathon"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)



//NewMarathonClient  creates a new Marathon client
func NewMarathonClient(marathonUrl string) (marathon.Marathon, error) {
	config := marathon.NewDefaultConfig()
	config.URL = marathonUrl
	//config.HTTPBasicAuthUser = username
	//config.HTTPBasicPassword = password
	config.HTTPClient = &http.Client{
		Timeout: (time.Duration(10) * time.Second),
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 10 * time.Second,
			}).Dial,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	client, err := marathon.NewClient(config)
	if err != nil {
		log.Errorf("Failed to create a client for marathon, marathonUrl: %s , error: %s ", marathonUrl, err.Error())
		return nil, err
	}

	return client, nil
}

// Controller communicates with Consul and monitors for changes
type Controller struct {
	client  marathon.Marathon
	monitor Monitor
}
// NewController creates a new Marathon controller
func NewController(addr string, interval time.Duration) (*Controller, error) {
	conf := marathon.NewDefaultConfig()
	conf.URL = addr

	client, err := NewMarathonClient(addr)
	return &Controller{
		monitor: NewMarathonMonitor(addr, interval),
		client:  client,
	}, err
}



// Services list declarations of all services in the system
func (c *Controller) Services() ([]*model.Service, error) {
	data, err := c.getServices()
	if err != nil {
		return nil, err
	}

	services := make([]*model.Service, 0, len(data.Apps))

	for _,instance := range data.Apps {
		endpoints, err := c.getMarathonService(instance.ID)
		if err != nil {
			return nil, err
		}
		services = append(services, convertService(endpoints))
	}

	return services, nil
}



// GetService retrieves a service by host name if it exists
func (c *Controller) GetService(hostname model.Hostname) (*model.Service, error) {
	// Get actual service by name

	name, err := parseHostname(hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", hostname, err)
		return nil, err
	}

	endpoints, err := c.getMarathonService(name)
	if len(endpoints) == 0 || err != nil {
		return nil, err
	}

	return convertService(endpoints), nil
}

func (c *Controller) getServices() (*marathon.Applications, error) {
	data, err := c.client.Applications(nil)
	if err != nil {
		log.Warnf("Could not retrieve services from consul: %v", err)
		return nil, err
	}

	return data, nil
}

func (c *Controller) getMarathonService(name string) ([]*marathon.Application, error) {
	endpoints, err := c.client.Application(name)
	if err != nil {
		log.Warnf("Could not retrieve service catalogue from consul: %v", err)
		return nil, err
	}
	endpoint := []*marathon.Application{endpoints}
	return endpoint, nil
}



// InstancesByPort retrieves instances for a service that match
// any of the supplied labels. All instances match an empty tag list.
func (c *Controller) InstancesByPort(hostname model.Hostname, port int,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	// Get actual service by name
	name,err := parseHostname(hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", hostname, err)
		return nil, err
	}

	endpoints, err := c.getMarathonService(name)
	if err != nil {
		return nil, err
	}

	instances := []*model.ServiceInstance{}
	for _, endpoint := range endpoints[0].Tasks {
		instance := convertInstance(endpoint)
		if labels.HasSubsetOf(instance.Labels) && portMatch(instance, port) {
		instances = append(instances, instance)
	}
	}

	return instances, nil
}

// returns true if an instance's port matches with any in the provided list
func portMatch(instance *model.ServiceInstance, port int) bool {
	return port == 0 || port == instance.Endpoint.ServicePort.Port
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	data, err := c.getServices()
	if err != nil {
		return nil, err
	}
	out := make([]*model.ServiceInstance, 0)
	for _,svcName := range data.Apps {
		tendpoints, err := c.getMarathonService(svcName.ID)
		endpoints := tendpoints[0].Tasks
		if err != nil {
			return nil, err
		}
		for _, endpoint := range endpoints {
			addr := endpoint.Host
			if addr == "" {
				addr = endpoint.Host
			}
			if node.IPAddress == addr {
				out = append(out, convertInstance(endpoint))
			}
		}
	}

	return out, nil
}


//finished ........TODO
// Run all controllers until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	c.monitor.Start(stop)
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.monitor.AppendServiceHandler(func(instances []*marathon.Application, event model.Event) error {
		f(convertService(instances), event)
		return nil
	})
	return nil
}

// AppendInstanceHandler implements a service catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.monitor.AppendInstanceHandler(func(instance *marathon.Task, event model.Event) error {
		f(convertInstance(instance), event)
		return nil
	})
	return nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation TODO
func (c *Controller) GetIstioServiceAccounts(hostname model.Hostname, ports []int) []string {
	// Need to get service account of service registered with consul
	// Currently Consul does not have service account or equivalent concept
	// As a step-1, to enabling istio security in Consul, We assume all the services run in default service account
	// This will allow all the consul services to do mTLS
	// Follow - https://goo.gl/Dt11Ct

	return []string{
		"spiffe://cluster.local/ns/default/sa/default",
	}
}
// ManagementPorts retrieves set of health check ports by instance IP.
// This does not apply to Consul service registry, as Consul does not
// manage the service instances. In future, when we integrate Nomad, we
// might revisit this function.
func (c *Controller) ManagementPorts(addr string) model.PortList {
	return nil
}

// WorkloadHealthCheckInfo retrieves set of health check info by instance IP.
// This does not apply to Consul service registry, as Consul does not
// manage the service instances. In future, when we integrate Nomad, we
// might revisit this function.
func (c *Controller) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	return nil
}