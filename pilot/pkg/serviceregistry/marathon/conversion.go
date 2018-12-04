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
	"fmt"
	"strings"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"github.com/gambol99/go-marathon"
)

const (
	protocolTagName = "protocol"
	externalTagName = "external"
)

func convertLabels(labels string) model.Labels {
	out := make(model.Labels, len(labels))
	if labels == "" {
		labels = "version|v1"
	} else {
		tlabels := "version|"
		tlabels += labels
		labels = tlabels
	}
	vals := strings.Split(labels, "|")
	if len(vals) > 1 {
		out[vals[0]] = vals[1]
	} else {
		log.Warnf("Tag %v ignored since it is not of form key|value", labels)
	}

	return out
}

func convertPort(port int, name string) *model.Port {
	if name == "" {
		name = "http"
	}

	return &model.Port{
		Name:     name,
		Port:     port,
		Protocol: convertProtocol(name),
	}
}

func convertService(endpoints []*marathon.Application) *model.Service {
	name := ""

	meshExternal := false
	resolution := model.ClientSideLB

	ports := make(map[int]*model.Port)
	for _, endpoint := range endpoints {
		name = strings.Split(endpoint.ID,"/")[1]
		mtpro := *endpoint.PortDefinitions
		port := convertPort(endpoint.Ports[0],mtpro[0].Protocol )

		if svcPort, exists := ports[port.Port]; exists && svcPort.Protocol != port.Protocol {
			log.Warnf("Service %v has two instances on same port %v but different protocols (%v, %v)",
				name, port.Port, svcPort.Protocol, port.Protocol)
		} else {
			ports[port.Port] = port
		}

		// TODO This will not work if service is a mix of external and local services
		// or if a service has more than one external name
		externalName := ""
		if externalName != ""  {
			meshExternal = true
			resolution = model.Passthrough
		}
	}

	svcPorts := make(model.PortList, 0, len(ports))
	for _, port := range ports {
		svcPorts = append(svcPorts, port)
	}

	hostname := serviceHostname(name)
	out := &model.Service{
		Hostname:     hostname,
		Address:      "0.0.0.0",
		Ports:        svcPorts,
		MeshExternal: meshExternal,
		Resolution:   resolution,
		Attributes: model.ServiceAttributes{
			Name:      string(hostname),
			Namespace: model.IstioDefaultConfigNamespace,
		},
	}

	return out
}

func convertInstance(instance *marathon.Task) *model.ServiceInstance {


	labels := convertLabels(instance.Version)
	port := instance.Ports[0]
	tport := convertPort(port,instance.IPAddresses[0].Protocol)
	addr := strings.Split(instance.AppID,"/")[1]
	if addr == "" {
		addr = instance.IPAddresses[0].IPAddress
	}

	meshExternal := false
	resolution := model.ClientSideLB
	externalName := ""
	if externalName != "" {
		meshExternal = true
		resolution = model.DNSLB
	}

	hostname := serviceHostname(addr)
	return &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address:     addr,
			Port:        instance.ServicePorts[0],
			ServicePort: tport,
		},
		AvailabilityZone: instance.Host,
		Service: &model.Service{
			Hostname:     hostname,
			Address:      addr,
			Ports:        model.PortList{tport},
			MeshExternal: meshExternal,
			Resolution:   resolution,
			Attributes: model.ServiceAttributes{
				Name:      string(hostname),
				Namespace: model.IstioDefaultConfigNamespace,
			},
		},
		Labels: labels,
	}
}

// serviceHostname produces FQDN for a consul service
func serviceHostname(name string) model.Hostname {
	// TODO include datacenter in Hostname?
	// consul DNS uses "redis.service.us-east-1.consul" -> "[<optional_tag>].<svc>.service.[<optional_datacenter>].consul"
	return model.Hostname(fmt.Sprintf("%s.service.marathon", name))
}

// parseHostname extracts service name from the service hostname
func parseHostname(hostname model.Hostname) (name string, err error) {
	parts := strings.Split(string(hostname), ".")
	if len(parts) < 1 || parts[0] == "" {
		err = fmt.Errorf("missing service name from the service hostname %q", hostname)
		return
	}
	name = "/" + parts[0]
	return
}

func convertProtocol(name string) model.Protocol {
	protocol := model.ParseProtocol(name)
	if protocol == model.ProtocolUnsupported {
		log.Warnf("unsupported protocol value: %s", name)
		return model.ProtocolTCP
	}
	return protocol
}
