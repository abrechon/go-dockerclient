// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStateString(t *testing.T) {
	t.Parallel()
	started := time.Now().Add(-3 * time.Hour)
	tests := []struct {
		name     string
		input    State
		expected string
	}{
		{"paused", State{Running: true, Paused: true, StartedAt: started}, "Up 3 hours (Paused)"},
		{"restarting", State{Running: true, Restarting: true, ExitCode: 7, FinishedAt: started}, "Restarting (7) 3 hours ago"},
		{"up", State{Running: true, StartedAt: started}, "Up 3 hours"},
		{"being removed", State{RemovalInProgress: true}, "Removal In Progress"},
		{"dead", State{Dead: true}, "Dead"},
		{"created", State{}, "Created"},
		{"no creation info", State{StartedAt: started}, ""},
		{"erro code", State{ExitCode: 7, StartedAt: started, FinishedAt: started}, "Exited (7) 3 hours ago"},
	}
	for _, tt := range tests {
		test := tt
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.input.String(); got != test.expected {
				t.Errorf("State.String(): wrong result. Want %q. Got %q.", test.expected, got)
			}
		})
	}
}

func TestStateStateString(t *testing.T) {
	t.Parallel()
	started := time.Now().Add(-3 * time.Hour)
	tests := []struct {
		input    State
		expected string
	}{
		{State{Running: true, Paused: true}, "paused"},
		{State{Running: true, Restarting: true}, "restarting"},
		{State{Running: true}, "running"},
		{State{Dead: true}, "dead"},
		{State{}, "created"},
		{State{StartedAt: started}, "exited"},
	}
	for _, tt := range tests {
		test := tt
		t.Run(test.expected, func(t *testing.T) {
			t.Parallel()
			if got := test.input.StateString(); got != test.expected {
				t.Errorf("State.String(): wrong result. Want %q. Got %q.", test.expected, got)
			}
		})
	}
}

func TestListContainers(t *testing.T) {
	t.Parallel()
	jsonContainers := `[
     {
             "Id": "8dfafdbc3a40",
             "Image": "base:latest",
             "Command": "echo 1",
             "Created": 1367854155,
             "Ports":[{"PrivatePort": 2222, "PublicPort": 3333, "Type": "tcp"}],
             "Status": "Exit 0"
     },
     {
             "Id": "9cd87474be90",
             "Image": "base:latest",
             "Command": "echo 222222",
             "Created": 1367854155,
             "Ports":[{"PrivatePort": 2222, "PublicPort": 3333, "Type": "tcp"}],
             "Status": "Exit 0"
     },
     {
             "Id": "3176a2479c92",
             "Image": "base:latest",
             "Command": "echo 3333333333333333",
             "Created": 1367854154,
             "Ports":[{"PrivatePort": 2221, "PublicPort": 3331, "Type": "tcp"}],
             "Status": "Exit 0"
     },
     {
             "Id": "4cb07b47f9fb",
             "Image": "base:latest",
             "Command": "echo 444444444444444444444444444444444",
             "Ports":[{"PrivatePort": 2223, "PublicPort": 3332, "Type": "tcp"}],
             "Created": 1367854152,
             "Status": "Exit 0"
     }
]`
	var expected []APIContainers
	err := json.Unmarshal([]byte(jsonContainers), &expected)
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(&FakeRoundTripper{message: jsonContainers, status: http.StatusOK})
	containers, err := client.ListContainers(ListContainersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(containers, expected) {
		t.Errorf("ListContainers: Expected %#v. Got %#v.", expected, containers)
	}
}

func TestListContainersParams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  ListContainersOptions
		params map[string][]string
	}{
		{ListContainersOptions{}, map[string][]string{}},
		{ListContainersOptions{All: true}, map[string][]string{"all": {"1"}}},
		{ListContainersOptions{All: true, Limit: 10}, map[string][]string{"all": {"1"}, "limit": {"10"}}},
		{
			ListContainersOptions{All: true, Limit: 10, Since: "adf9983", Before: "abdeef"},
			map[string][]string{"all": {"1"}, "limit": {"10"}, "since": {"adf9983"}, "before": {"abdeef"}},
		},
		{
			ListContainersOptions{Filters: map[string][]string{"status": {"paused", "running"}}},
			map[string][]string{"filters": {"{\"status\":[\"paused\",\"running\"]}"}},
		},
		{
			ListContainersOptions{All: true, Filters: map[string][]string{"exited": {"0"}, "status": {"exited"}}},
			map[string][]string{"all": {"1"}, "filters": {"{\"exited\":[\"0\"],\"status\":[\"exited\"]}"}},
		},
	}
	const expectedPath = "/containers/json"
	for _, tt := range tests {
		test := tt
		t.Run("", func(t *testing.T) {
			t.Parallel()
			fakeRT := &FakeRoundTripper{message: "[]", status: http.StatusOK}
			client := newTestClient(fakeRT)
			if _, err := client.ListContainers(test.input); err != nil {
				t.Error(err)
			}
			got := map[string][]string(fakeRT.requests[0].URL.Query())
			if !reflect.DeepEqual(got, test.params) {
				t.Errorf("Expected %#v, got %#v.", test.params, got)
			}
			if path := fakeRT.requests[0].URL.Path; path != expectedPath {
				t.Errorf("Wrong path on request. Want %q. Got %q.", expectedPath, path)
			}
			if meth := fakeRT.requests[0].Method; meth != "GET" {
				t.Errorf("Wrong HTTP method. Want GET. Got %s.", meth)
			}
		})
	}
}

func TestListContainersFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status  int
		message string
	}{
		{400, "bad parameter"},
		{500, "internal server error"},
	}
	for _, tt := range tests {
		test := tt
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			t.Parallel()
			client := newTestClient(&FakeRoundTripper{message: test.message, status: test.status})
			expected := Error{Status: test.status, Message: test.message}
			containers, err := client.ListContainers(ListContainersOptions{})
			if !reflect.DeepEqual(expected, *err.(*Error)) {
				t.Errorf("Wrong error in ListContainers. Want %#v. Got %#v.", expected, err)
			}
			if len(containers) > 0 {
				t.Errorf("ListContainers failure. Expected empty list. Got %#v.", containers)
			}
		})
	}
}

func TestInspectContainer(t *testing.T) {
	t.Parallel()
	jsonContainer := `{
             "Id": "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2",
             "AppArmorProfile": "Profile",
             "Created": "2013-05-07T14:51:42.087658+02:00",
             "Path": "date",
             "Args": [],
             "Config": {
                     "Hostname": "4fa6e0f0c678",
                     "User": "",
                     "Memory": 17179869184,
                     "MemorySwap": 34359738368,
                     "AttachStdin": false,
                     "AttachStdout": true,
                     "AttachStderr": true,
                     "PortSpecs": null,
                     "Tty": false,
                     "OpenStdin": false,
                     "StdinOnce": false,
                     "Env": null,
                     "Cmd": [
                             "date"
                     ],
                     "Image": "base",
                     "Volumes": {},
                     "VolumesFrom": "",
                     "SecurityOpt": [
                         "label:user:USER"
                      ],
                      "Ulimits": [
                          { "Name": "nofile", "Soft": 1024, "Hard": 2048 }
											],
											"Shell": [
                         "/bin/sh", "-c"
											]
             },
             "State": {
                     "Running": false,
                     "Pid": 0,
                     "ExitCode": 0,
                     "StartedAt": "2013-05-07T14:51:42.087658+02:00",
                     "Ghost": false
             },
             "Node": {
                  "ID": "4I4E:QR4I:Z733:QEZK:5X44:Q4T7:W2DD:JRDY:KB2O:PODO:Z5SR:XRB6",
                  "IP": "192.168.99.105",
                  "Addra": "192.168.99.105:2376",
                  "Name": "node-01",
                  "Cpus": 4,
                  "Memory": 1048436736,
                  "Labels": {
                      "executiondriver": "native-0.2",
                      "kernelversion": "3.18.5-tinycore64",
                      "operatingsystem": "Boot2Docker 1.5.0 (TCL 5.4); master : a66bce5 - Tue Feb 10 23:31:27 UTC 2015",
                      "provider": "virtualbox",
                      "storagedriver": "aufs"
                  }
              },
             "Image": "b750fe79269d2ec9a3c593ef05b4332b1d1a02a62b4accb2c21d589ff2f5f2dc",
             "NetworkSettings": {
                     "IpAddress": "",
                     "IpPrefixLen": 0,
                     "Gateway": "",
                     "Bridge": "",
                     "PortMapping": null
             },
             "SysInitPath": "/home/kitty/go/src/github.com/dotcloud/docker/bin/docker",
             "ResolvConfPath": "/etc/resolv.conf",
             "Volumes": {},
             "HostConfig": {
               "Binds": null,
               "ContainerIDFile": "",
               "LxcConf": [],
               "Privileged": false,
               "PortBindings": {
                 "80/tcp": [
                   {
                     "HostIp": "0.0.0.0",
                     "HostPort": "49153"
                   }
                 ]
               },
               "Links": null,
               "PublishAllPorts": false,
               "CgroupParent": "/mesos",
               "Memory": 17179869184,
               "MemorySwap": 34359738368,
               "GroupAdd": ["fake", "12345"],
               "OomScoreAdj": 642
             }
}`
	var expected Container
	err := json.Unmarshal([]byte(jsonContainer), &expected)
	if err != nil {
		t.Fatal(err)
	}
	fakeRT := &FakeRoundTripper{message: jsonContainer, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c678"
	container, err := client.InspectContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*container, expected) {
		t.Errorf("InspectContainer(%q): Expected %#v. Got %#v.", id, expected, container)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/4fa6e0f0c678/json"))
	if gotPath := fakeRT.requests[0].URL.Path; gotPath != expectedURL.Path {
		t.Errorf("InspectContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestInspectContainerWithContext(t *testing.T) {
	t.Parallel()
	jsonContainer := `{
             "Id": "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2",
             "AppArmorProfile": "Profile",
             "Created": "2013-05-07T14:51:42.087658+02:00",
             "Path": "date",
             "Args": [],
             "Config": {
                     "Hostname": "4fa6e0f0c678",
                     "User": "",
                     "Memory": 17179869184,
                     "MemorySwap": 34359738368,
                     "AttachStdin": false,
                     "AttachStdout": true,
                     "AttachStderr": true,
                     "PortSpecs": null,
                     "Tty": false,
                     "OpenStdin": false,
                     "StdinOnce": false,
                     "Env": null,
                     "Cmd": [
                             "date"
                     ],
                     "Image": "base",
                     "Volumes": {},
                     "VolumesFrom": "",
                     "SecurityOpt": [
                         "label:user:USER"
                      ],
                      "Ulimits": [
                          { "Name": "nofile", "Soft": 1024, "Hard": 2048 }
                      ]
             },
             "State": {
                     "Running": false,
                     "Pid": 0,
                     "ExitCode": 0,
                     "StartedAt": "2013-05-07T14:51:42.087658+02:00",
                     "Ghost": false
             },
             "Node": {
                  "ID": "4I4E:QR4I:Z733:QEZK:5X44:Q4T7:W2DD:JRDY:KB2O:PODO:Z5SR:XRB6",
                  "IP": "192.168.99.105",
                  "Addra": "192.168.99.105:2376",
                  "Name": "node-01",
                  "Cpus": 4,
                  "Memory": 1048436736,
                  "Labels": {
                      "executiondriver": "native-0.2",
                      "kernelversion": "3.18.5-tinycore64",
                      "operatingsystem": "Boot2Docker 1.5.0 (TCL 5.4); master : a66bce5 - Tue Feb 10 23:31:27 UTC 2015",
                      "provider": "virtualbox",
                      "storagedriver": "aufs"
                  }
              },
             "Image": "b750fe79269d2ec9a3c593ef05b4332b1d1a02a62b4accb2c21d589ff2f5f2dc",
             "NetworkSettings": {
                     "IpAddress": "",
                     "IpPrefixLen": 0,
                     "Gateway": "",
                     "Bridge": "",
                     "PortMapping": null
             },
             "SysInitPath": "/home/kitty/go/src/github.com/dotcloud/docker/bin/docker",
             "ResolvConfPath": "/etc/resolv.conf",
             "Volumes": {},
             "HostConfig": {
               "Binds": null,
               "BlkioDeviceReadIOps": [
                   {
                       "Path": "/dev/sdb",
                       "Rate": 100
                   }
               ],
               "BlkioDeviceWriteBps": [
                   {
                       "Path": "/dev/sdb",
                       "Rate": 5000
                   }
               ],
               "ContainerIDFile": "",
               "LxcConf": [],
               "Privileged": false,
               "PortBindings": {
                 "80/tcp": [
                   {
                     "HostIp": "0.0.0.0",
                     "HostPort": "49153"
                   }
                 ]
               },
               "Links": null,
               "PublishAllPorts": false,
               "CgroupParent": "/mesos",
               "Memory": 17179869184,
               "MemorySwap": 34359738368,
               "GroupAdd": ["fake", "12345"],
               "OomScoreAdj": 642
             }
}`
	var expected Container
	err := json.Unmarshal([]byte(jsonContainer), &expected)
	if err != nil {
		t.Fatal(err)
	}
	fakeRT := &FakeRoundTripper{message: jsonContainer, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c678"

	ctx, cancel := context.WithTimeout(context.TODO(), 1*time.Second)
	defer cancel()

	inspectError := make(chan error)
	// Invoke InspectContainer in a goroutine. The response is sent to the 'inspectError'
	// channel.
	go func() {
		container, err := client.InspectContainer(id)
		if err != nil {
			inspectError <- err
			return
		}
		if !reflect.DeepEqual(*container, expected) {
			inspectError <- fmt.Errorf("inspectContainer(%q): Expected %#v. Got %#v", id, expected, container)
			return
		}
		expectedURL, _ := url.Parse(client.getURL("/containers/4fa6e0f0c678/json"))
		if gotPath := fakeRT.requests[0].URL.Path; gotPath != expectedURL.Path {
			inspectError <- fmt.Errorf("inspectContainer(%q): Wrong path in request. Want %q. Got %q", id, expectedURL.Path, gotPath)
			return
		}
		// No errors to tbe reported. Send 'nil'
		inspectError <- nil
	}()
	// Wait for either the inspect response or for the context.
	select {
	case err := <-inspectError:
		if err != nil {
			t.Fatalf("Error inspecting container with context: %v", err)
		}
	case <-ctx.Done():
		// Context was canceled unexpectedly. Report the same.
		t.Fatalf("Context canceled when waiting for inspect container response: %v", ctx.Err())
	}
}

func TestInspectContainerNetwork(t *testing.T) {
	t.Parallel()
	jsonContainer := `{
            "Id": "81e1bbe20b5508349e1c804eb08b7b6ca8366751dbea9f578b3ea0773fa66c1c",
            "Created": "2015-11-12T14:54:04.791485659Z",
            "Path": "consul-template",
            "Args": [
                "-config=/tmp/haproxy.json",
                "-consul=192.168.99.120:8500"
            ],
            "State": {
                "Status": "running",
                "Running": true,
                "Paused": false,
                "Restarting": false,
                "OOMKilled": false,
                "Dead": false,
                "Pid": 3196,
                "ExitCode": 0,
                "Error": "",
                "StartedAt": "2015-11-12T14:54:05.026747471Z",
                "FinishedAt": "0001-01-01T00:00:00Z"
            },
            "Image": "4921c5917fc117df3dec32f4c1976635dc6c56ccd3336fe1db3477f950e78bf7",
            "ResolvConfPath": "/mnt/sda1/var/lib/docker/containers/81e1bbe20b5508349e1c804eb08b7b6ca8366751dbea9f578b3ea0773fa66c1c/resolv.conf",
            "HostnamePath": "/mnt/sda1/var/lib/docker/containers/81e1bbe20b5508349e1c804eb08b7b6ca8366751dbea9f578b3ea0773fa66c1c/hostname",
            "HostsPath": "/mnt/sda1/var/lib/docker/containers/81e1bbe20b5508349e1c804eb08b7b6ca8366751dbea9f578b3ea0773fa66c1c/hosts",
            "LogPath": "/mnt/sda1/var/lib/docker/containers/81e1bbe20b5508349e1c804eb08b7b6ca8366751dbea9f578b3ea0773fa66c1c/81e1bbe20b5508349e1c804eb08b7b6ca8366751dbea9f578b3ea0773fa66c1c-json.log",
            "Node": {
                "ID": "AUIB:LFOT:3LSF:SCFS:OYDQ:NLXD:JZNE:4INI:3DRC:ZFBB:GWCY:DWJK",
                "IP": "192.168.99.121",
                "Addr": "192.168.99.121:2376",
                "Name": "swl-demo1",
                "Cpus": 1,
                "Memory": 2099945472,
                "Labels": {
                    "executiondriver": "native-0.2",
                    "kernelversion": "4.1.12-boot2docker",
                    "operatingsystem": "Boot2Docker 1.9.0 (TCL 6.4); master : 16e4a2a - Tue Nov  3 19:49:22 UTC 2015",
                    "provider": "virtualbox",
                    "storagedriver": "aufs"
                }
            },
            "Name": "/docker-proxy.swl-demo1",
            "RestartCount": 0,
            "Driver": "aufs",
            "ExecDriver": "native-0.2",
            "MountLabel": "",
            "ProcessLabel": "",
            "AppArmorProfile": "",
            "ExecIDs": null,
            "HostConfig": {
                "Binds": null,
                "ContainerIDFile": "",
                "LxcConf": [],
                "Memory": 0,
                "MemoryReservation": 0,
                "MemorySwap": 0,
                "KernelMemory": 0,
                "CpuShares": 0,
                "CpuPeriod": 0,
                "CpusetCpus": "",
                "CpusetMems": "",
                "CpuQuota": 0,
                "BlkioWeight": 0,
                "OomKillDisable": false,
                "MemorySwappiness": -1,
                "Privileged": false,
                "PortBindings": {
                    "443/tcp": [
                        {
                            "HostIp": "",
                            "HostPort": "443"
                        }
                    ]
                },
                "Links": null,
                "PublishAllPorts": false,
                "Dns": null,
                "DnsOptions": null,
                "DnsSearch": null,
                "ExtraHosts": null,
                "VolumesFrom": null,
                "Devices": [],
                "NetworkMode": "swl-net",
                "IpcMode": "",
                "PidMode": "",
                "UTSMode": "",
                "CapAdd": null,
                "CapDrop": null,
                "GroupAdd": null,
                "RestartPolicy": {
                    "Name": "no",
                    "MaximumRetryCount": 0
                },
                "SecurityOpt": null,
                "ReadonlyRootfs": false,
                "Ulimits": null,
                "LogConfig": {
                    "Type": "json-file",
                    "Config": {}
                },
                "CgroupParent": "",
                "ConsoleSize": [
                    0,
                    0
                ],
                "VolumeDriver": ""
            },
            "GraphDriver": {
                "Name": "aufs",
                "Data": null
            },
            "Mounts": [],
            "Config": {
                "Hostname": "81e1bbe20b55",
                "Domainname": "",
                "User": "",
                "AttachStdin": false,
                "AttachStdout": false,
                "AttachStderr": false,
                "ExposedPorts": {
                    "443/tcp": {}
                },
                "Tty": false,
                "OpenStdin": false,
                "StdinOnce": false,
                "Env": [
                    "DOMAIN=local.auto",
                    "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                    "CONSUL_TEMPLATE_VERSION=0.11.1"
                ],
                "Cmd": [
                    "-consul=192.168.99.120:8500"
                ],
                "Image": "docker-proxy:latest",
                "Volumes": null,
                "WorkingDir": "",
                "Entrypoint": [
                    "consul-template",
                    "-config=/tmp/haproxy.json"
                ],
                "OnBuild": null,
                "Labels": {},
                "StopSignal": "SIGTERM"
            },
            "NetworkSettings": {
                "Bridge": "",
                "SandboxID": "c6b903dc5c1a96113a22dbc44709e30194079bd2d262eea1eb4f38d85821f6e1",
                "HairpinMode": false,
                "LinkLocalIPv6Address": "",
                "LinkLocalIPv6PrefixLen": 0,
                "Ports": {
                    "443/tcp": [
                        {
                            "HostIp": "192.168.99.121",
                            "HostPort": "443"
                        }
                    ]
                },
                "SandboxKey": "/var/run/docker/netns/c6b903dc5c1a",
                "SecondaryIPAddresses": null,
                "SecondaryIPv6Addresses": null,
                "EndpointID": "",
                "Gateway": "",
                "GlobalIPv6Address": "",
                "GlobalIPv6PrefixLen": 0,
                "IPAddress": "",
                "IPPrefixLen": 0,
                "IPv6Gateway": "",
                "MacAddress": "",
                "Networks": {
                    "swl-net": {
						"Aliases": [
							"testalias",
							"81e1bbe20b55"
						],
                        "NetworkID": "7ea29fc1412292a2d7bba362f9253545fecdfa8ce9a6e37dd10ba8bee7129812",
                        "EndpointID": "683e3092275782a53c3b0968cc7e3a10f23264022ded9cb20490902f96fc5981",
                        "Gateway": "",
                        "IPAddress": "10.0.0.3",
                        "IPPrefixLen": 24,
                        "IPv6Gateway": "",
                        "GlobalIPv6Address": "",
                        "GlobalIPv6PrefixLen": 0,
                        "MacAddress": "02:42:0a:00:00:03"
                    }
                }
            }
}`

	fakeRT := &FakeRoundTripper{message: jsonContainer, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "81e1bbe20b55"
	expIP := "10.0.0.3"
	expNetworkID := "7ea29fc1412292a2d7bba362f9253545fecdfa8ce9a6e37dd10ba8bee7129812"
	expectedAliases := []string{"testalias", "81e1bbe20b55"}

	container, err := client.InspectContainer(id)
	if err != nil {
		t.Fatal(err)
	}

	s := reflect.Indirect(reflect.ValueOf(container.NetworkSettings))
	networks := s.FieldByName("Networks")
	if networks.IsValid() {
		var ip string
		for _, net := range networks.MapKeys() {
			if net.Interface().(string) == container.HostConfig.NetworkMode {
				ip = networks.MapIndex(net).FieldByName("IPAddress").Interface().(string)
				t.Logf("%s %v", net, ip)
			}
		}
		if ip != expIP {
			t.Errorf("InspectContainerNetworks(%q): Expected %#v. Got %#v.", id, expIP, ip)
		}

		var networkID string
		for _, net := range networks.MapKeys() {
			if net.Interface().(string) == container.HostConfig.NetworkMode {
				networkID = networks.MapIndex(net).FieldByName("NetworkID").Interface().(string)
				t.Logf("%s %v", net, networkID)
			}
		}

		var aliases []string
		for _, net := range networks.MapKeys() {
			if net.Interface().(string) == container.HostConfig.NetworkMode {
				aliases = networks.MapIndex(net).FieldByName("Aliases").Interface().([]string)
			}
		}
		if !reflect.DeepEqual(aliases, expectedAliases) {
			t.Errorf("InspectContainerNetworks(%q): Expected Aliases %#v. Got %#v.", id, expectedAliases, aliases)
		}

		if networkID != expNetworkID {
			t.Errorf("InspectContainerNetworks(%q): Expected %#v. Got %#v.", id, expNetworkID, networkID)
		}
	} else {
		t.Errorf("InspectContainerNetworks(%q): No method Networks for NetworkSettings", id)
	}
}

func TestInspectContainerNegativeSwap(t *testing.T) {
	t.Parallel()
	jsonContainer := `{
             "Id": "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2",
             "Created": "2013-05-07T14:51:42.087658+02:00",
             "Path": "date",
             "Args": [],
             "Config": {
                     "Hostname": "4fa6e0f0c678",
                     "User": "",
                     "Memory": 17179869184,
                     "MemorySwap": -1,
                     "AttachStdin": false,
                     "AttachStdout": true,
                     "AttachStderr": true,
                     "PortSpecs": null,
                     "Tty": false,
                     "OpenStdin": false,
                     "StdinOnce": false,
                     "Env": null,
                     "Cmd": [
                             "date"
                     ],
                     "Image": "base",
                     "Volumes": {},
                     "VolumesFrom": ""
             },
             "State": {
                     "Running": false,
                     "Pid": 0,
                     "ExitCode": 0,
                     "StartedAt": "2013-05-07T14:51:42.087658+02:00",
                     "Ghost": false
             },
             "Image": "b750fe79269d2ec9a3c593ef05b4332b1d1a02a62b4accb2c21d589ff2f5f2dc",
             "NetworkSettings": {
                     "IpAddress": "",
                     "IpPrefixLen": 0,
                     "Gateway": "",
                     "Bridge": "",
                     "PortMapping": null
             },
             "SysInitPath": "/home/kitty/go/src/github.com/dotcloud/docker/bin/docker",
             "ResolvConfPath": "/etc/resolv.conf",
             "Volumes": {},
             "HostConfig": {
               "Binds": null,
               "ContainerIDFile": "",
               "LxcConf": [],
               "Privileged": false,
               "PortBindings": {
                 "80/tcp": [
                   {
                     "HostIp": "0.0.0.0",
                     "HostPort": "49153"
                   }
                 ]
               },
               "Links": null,
               "PublishAllPorts": false
             }
}`
	var expected Container
	err := json.Unmarshal([]byte(jsonContainer), &expected)
	if err != nil {
		t.Fatal(err)
	}
	fakeRT := &FakeRoundTripper{message: jsonContainer, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c678"
	container, err := client.InspectContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*container, expected) {
		t.Errorf("InspectContainer(%q): Expected %#v. Got %#v.", id, expected, container)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/4fa6e0f0c678/json"))
	if gotPath := fakeRT.requests[0].URL.Path; gotPath != expectedURL.Path {
		t.Errorf("InspectContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestInspectContainerFailure(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "server error", status: 500})
	expected := Error{Status: 500, Message: "server error"}
	container, err := client.InspectContainer("abe033")
	if container != nil {
		t.Errorf("InspectContainer: Expected <nil> container, got %#v", container)
	}
	if !reflect.DeepEqual(expected, *err.(*Error)) {
		t.Errorf("InspectContainer: Wrong error information. Want %#v. Got %#v.", expected, err)
	}
}

func TestInspectContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: 404})
	container, err := client.InspectContainer("abe033")
	if container != nil {
		t.Errorf("InspectContainer: Expected <nil> container, got %#v", container)
	}
	expected := &NoSuchContainer{ID: "abe033"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("InspectContainer: Wrong error information. Want %#v. Got %#v.", expected, err)
	}
}

func TestContainerChanges(t *testing.T) {
	t.Parallel()
	jsonChanges := `[
     {
             "Path":"/dev",
             "Kind":0
     },
     {
             "Path":"/dev/kmsg",
             "Kind":1
     },
     {
             "Path":"/test",
             "Kind":1
     }
]`
	var expected []Change
	err := json.Unmarshal([]byte(jsonChanges), &expected)
	if err != nil {
		t.Fatal(err)
	}
	fakeRT := &FakeRoundTripper{message: jsonChanges, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c678"
	changes, err := client.ContainerChanges(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changes, expected) {
		t.Errorf("ContainerChanges(%q): Expected %#v. Got %#v.", id, expected, changes)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/4fa6e0f0c678/changes"))
	if gotPath := fakeRT.requests[0].URL.Path; gotPath != expectedURL.Path {
		t.Errorf("ContainerChanges(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestContainerChangesFailure(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "server error", status: 500})
	expected := Error{Status: 500, Message: "server error"}
	changes, err := client.ContainerChanges("abe033")
	if changes != nil {
		t.Errorf("ContainerChanges: Expected <nil> changes, got %#v", changes)
	}
	if !reflect.DeepEqual(expected, *err.(*Error)) {
		t.Errorf("ContainerChanges: Wrong error information. Want %#v. Got %#v.", expected, err)
	}
}

func TestContainerChangesNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: 404})
	changes, err := client.ContainerChanges("abe033")
	if changes != nil {
		t.Errorf("ContainerChanges: Expected <nil> changes, got %#v", changes)
	}
	expected := &NoSuchContainer{ID: "abe033"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("ContainerChanges: Wrong error information. Want %#v. Got %#v.", expected, err)
	}
}

func TestCreateContainer(t *testing.T) {
	t.Parallel()
	jsonContainer := `{
             "Id": "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2",
	     "Warnings": []
}`
	var expected Container
	err := json.Unmarshal([]byte(jsonContainer), &expected)
	if err != nil {
		t.Fatal(err)
	}
	fakeRT := &FakeRoundTripper{message: jsonContainer, status: http.StatusOK}
	client := newTestClient(fakeRT)
	config := Config{AttachStdout: true, AttachStdin: true}
	opts := CreateContainerOptions{Name: "TestCreateContainer", Config: &config}
	container, err := client.CreateContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	if container.ID != id {
		t.Errorf("CreateContainer: wrong ID. Want %q. Got %q.", id, container.ID)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("CreateContainer: wrong HTTP method. Want %q. Got %q.", "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/create"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("CreateContainer: Wrong path in request. Want %q. Got %q.", expectedURL.Path, gotPath)
	}
	var gotBody Config
	err = json.NewDecoder(req.Body).Decode(&gotBody)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateContainerImageNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "No such image: whatever", status: http.StatusNotFound})
	config := Config{AttachStdout: true, AttachStdin: true}
	container, err := client.CreateContainer(CreateContainerOptions{Config: &config})
	if container != nil {
		t.Errorf("CreateContainer: expected <nil> container, got %#v.", container)
	}
	if !reflect.DeepEqual(err, ErrNoSuchImage) {
		t.Errorf("CreateContainer: Wrong error type. Want %#v. Got %#v.", ErrNoSuchImage, err)
	}
}

func TestCreateContainerDuplicateName(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "No such image", status: http.StatusConflict})
	config := Config{AttachStdout: true, AttachStdin: true}
	container, err := client.CreateContainer(CreateContainerOptions{Config: &config})
	if container != nil {
		t.Errorf("CreateContainer: expected <nil> container, got %#v.", container)
	}
	if err != ErrContainerAlreadyExists {
		t.Errorf("CreateContainer: Wrong error type. Want %#v. Got %#v.", ErrContainerAlreadyExists, err)
	}
}

// Workaround for 17.09 bug returning 400 instead of 409.
// See https://github.com/moby/moby/issues/35021
func TestCreateContainerDuplicateNameWorkaroundDocker17_09(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: `{"message":"Conflict. The container name \"/c1\" is already in use by container \"2ce137e165dfca5e087f247b5d05a2311f91ef3da4bb7772168446a1a47e2f68\". You have to remove (or rename) that container to be able to reuse that name."}`, status: http.StatusBadRequest})
	config := Config{AttachStdout: true, AttachStdin: true}
	container, err := client.CreateContainer(CreateContainerOptions{Config: &config})
	if container != nil {
		t.Errorf("CreateContainer: expected <nil> container, got %#v.", container)
	}
	if err != ErrContainerAlreadyExists {
		t.Errorf("CreateContainer: Wrong error type. Want %#v. Got %#v.", ErrContainerAlreadyExists, err)
	}
}

func TestCreateContainerWithHostConfig(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "{}", status: http.StatusOK}
	client := newTestClient(fakeRT)
	config := Config{}
	hostConfig := HostConfig{PublishAllPorts: true}
	opts := CreateContainerOptions{Name: "TestCreateContainerWithHostConfig", Config: &config, HostConfig: &hostConfig}
	_, err := client.CreateContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	var gotBody map[string]interface{}
	err = json.NewDecoder(req.Body).Decode(&gotBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["HostConfig"]; !ok {
		t.Errorf("CreateContainer: wrong body. HostConfig was not serialized")
	}
}

func TestUpdateContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	update := UpdateContainerOptions{Memory: 12345, CpusetMems: "0,1"}
	err := client.UpdateContainer(id, update)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("UpdateContainer: wrong HTTP method. Want %q. Got %q.", "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/update"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("UpdateContainer: Wrong path in request. Want %q. Got %q.", expectedURL.Path, gotPath)
	}
	expectedContentType := "application/json"
	if contentType := req.Header.Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("UpdateContainer: Wrong content-type in request. Want %q. Got %q.", expectedContentType, contentType)
	}
	var out UpdateContainerOptions
	if err := json.NewDecoder(req.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, update) {
		t.Errorf("UpdateContainer: wrong body, got: %#v, want %#v", out, update)
	}
}

func TestStartContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.StartContainer(id, &HostConfig{})
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("StartContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/start"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("StartContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
	expectedContentType := "application/json"
	if contentType := req.Header.Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("StartContainer(%q): Wrong content-type in request. Want %q. Got %q.", id, expectedContentType, contentType)
	}
}

func TestStartContainerHostConfigAPI124(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	client.serverAPIVersion = apiVersion124
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.StartContainer(id, &HostConfig{})
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("StartContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/start"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("StartContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
	notAcceptedContentType := "application/json"
	if contentType := req.Header.Get("Content-Type"); contentType == notAcceptedContentType {
		t.Errorf("StartContainer(%q): Unepected %q Content-Type in request.", id, contentType)
	}
	if req.Body != nil {
		data, _ := ioutil.ReadAll(req.Body)
		t.Errorf("StartContainer(%q): Unexpected data sent: %s", id, data)
	}
}

func TestStartContainerNilHostConfig(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.StartContainer(id, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("StartContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/start"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("StartContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
	expectedContentType := "application/json"
	if contentType := req.Header.Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("StartContainer(%q): Wrong content-type in request. Want %q. Got %q.", id, expectedContentType, contentType)
	}
	var buf [4]byte
	req.Body.Read(buf[:])
	if string(buf[:]) != "null" {
		t.Errorf("Startcontainer(%q): Wrong body. Want null. Got %s", id, buf[:])
	}
}

func TestStartContainerWithContext(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"

	ctx, cancel := context.WithTimeout(context.TODO(), 1*time.Second)
	defer cancel()

	startError := make(chan error)
	go func() {
		startError <- client.StartContainerWithContext(id, &HostConfig{}, ctx)
	}()
	select {
	case err := <-startError:
		if err != nil {
			t.Fatal(err)
		}
		req := fakeRT.requests[0]
		if req.Method != "POST" {
			t.Errorf("StartContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
		}
		expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/start"))
		if gotPath := req.URL.Path; gotPath != expectedURL.Path {
			t.Errorf("StartContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
		}
		expectedContentType := "application/json"
		if contentType := req.Header.Get("Content-Type"); contentType != expectedContentType {
			t.Errorf("StartContainer(%q): Wrong content-type in request. Want %q. Got %q.", id, expectedContentType, contentType)
		}
	case <-ctx.Done():
		// Context was canceled unexpectedly. Report the same.
		t.Fatalf("Context canceled when waiting for start container response: %v", ctx.Err())
	}
}

func TestStartContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.StartContainer("a2344", &HostConfig{})
	expected := &NoSuchContainer{ID: "a2344", Err: err.(*NoSuchContainer).Err}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("StartContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestStartContainerAlreadyRunning(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "container already running", status: http.StatusNotModified})
	err := client.StartContainer("a2334", &HostConfig{})
	expected := &ContainerAlreadyRunning{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("StartContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestStopContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.StopContainer(id, 10)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("StopContainer(%q, 10): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/stop"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("StopContainer(%q, 10): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestStopContainerWithContext(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"

	ctx, cancel := context.WithTimeout(context.TODO(), 1*time.Second)
	defer cancel()

	stopError := make(chan error)
	go func() {
		stopError <- client.StopContainerWithContext(id, 10, ctx)
	}()
	select {
	case err := <-stopError:
		if err != nil {
			t.Fatal(err)
		}
		req := fakeRT.requests[0]
		if req.Method != "POST" {
			t.Errorf("StopContainer(%q, 10): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
		}
		expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/stop"))
		if gotPath := req.URL.Path; gotPath != expectedURL.Path {
			t.Errorf("StopContainer(%q, 10): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
		}
	case <-ctx.Done():
		// Context was canceled unexpectedly. Report the same.
		t.Fatalf("Context canceled when waiting for stop container response: %v", ctx.Err())
	}
}

func TestStopContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.StopContainer("a2334", 10)
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("StopContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestStopContainerNotRunning(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "container not running", status: http.StatusNotModified})
	err := client.StopContainer("a2334", 10)
	expected := &ContainerNotRunning{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("StopContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestRestartContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.RestartContainer(id, 10)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("RestartContainer(%q, 10): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/restart"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("RestartContainer(%q, 10): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestRestartContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.RestartContainer("a2334", 10)
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("RestartContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestPauseContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.PauseContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("PauseContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/pause"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("PauseContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestPauseContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.PauseContainer("a2334")
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("PauseContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestUnpauseContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.UnpauseContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("PauseContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/unpause"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("PauseContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestUnpauseContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.UnpauseContainer("a2334")
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("PauseContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestKillContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.KillContainer(KillContainerOptions{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("KillContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/kill"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("KillContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestKillContainerSignal(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.KillContainer(KillContainerOptions{ID: id, Signal: SIGTERM})
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("KillContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	if signal := req.URL.Query().Get("signal"); signal != "15" {
		t.Errorf("KillContainer(%q): Wrong query string in request. Want %q. Got %q.", id, "15", signal)
	}
}

func TestKillContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.KillContainer(KillContainerOptions{ID: "a2334"})
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("KillContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestKillContainerNotRunning(t *testing.T) {
	t.Parallel()
	id := "abcd1234567890"
	msg := fmt.Sprintf("Cannot kill container: %[1]s: Container %[1]s is not running", id)
	client := newTestClient(&FakeRoundTripper{message: msg, status: http.StatusConflict})
	err := client.KillContainer(KillContainerOptions{ID: id})
	expected := &ContainerNotRunning{ID: id}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("KillContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestRemoveContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	opts := RemoveContainerOptions{ID: id}
	err := client.RemoveContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "DELETE" {
		t.Errorf("RemoveContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "DELETE", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("RemoveContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestRemoveContainerRemoveVolumes(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	opts := RemoveContainerOptions{ID: id, RemoveVolumes: true}
	err := client.RemoveContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	params := map[string][]string(req.URL.Query())
	expected := map[string][]string{"v": {"1"}}
	if !reflect.DeepEqual(params, expected) {
		t.Errorf("RemoveContainer(%q): wrong parameters. Want %#v. Got %#v.", id, expected, params)
	}
}

func TestRemoveContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.RemoveContainer(RemoveContainerOptions{ID: "a2334"})
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("RemoveContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestResizeContainerTTY(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.ResizeContainerTTY(id, 40, 80)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("ResizeContainerTTY(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/resize"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("ResizeContainerTTY(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
	got := map[string][]string(req.URL.Query())
	expectedParams := map[string][]string{
		"w": {"80"},
		"h": {"40"},
	}
	if !reflect.DeepEqual(got, expectedParams) {
		t.Errorf("Expected %#v, got %#v.", expectedParams, got)
	}
}

func TestWaitContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: `{"StatusCode": 56}`, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	status, err := client.WaitContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	if status != 56 {
		t.Errorf("WaitContainer(%q): wrong return. Want 56. Got %d.", id, status)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("WaitContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/wait"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("WaitContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestWaitContainerWithContext(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: `{"StatusCode": 56}`, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"

	ctx, cancel := context.WithTimeout(context.TODO(), 1*time.Second)
	defer cancel()

	var status int
	waitError := make(chan error)
	go func() {
		var err error
		status, err = client.WaitContainerWithContext(id, ctx)
		waitError <- err
	}()
	select {
	case err := <-waitError:
		if err != nil {
			t.Fatal(err)
		}
		if status != 56 {
			t.Errorf("WaitContainer(%q): wrong return. Want 56. Got %d.", id, status)
		}
		req := fakeRT.requests[0]
		if req.Method != "POST" {
			t.Errorf("WaitContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
		}
		expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/wait"))
		if gotPath := req.URL.Path; gotPath != expectedURL.Path {
			t.Errorf("WaitContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
		}
	case <-ctx.Done():
		// Context was canceled unexpectedly. Report the same.
		t.Fatalf("Context canceled when waiting for wait container response: %v", ctx.Err())
	}
}

func TestWaitContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	_, err := client.WaitContainer("a2334")
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("WaitContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestCommitContainer(t *testing.T) {
	t.Parallel()
	response := `{"Id":"596069db4bf5"}`
	client := newTestClient(&FakeRoundTripper{message: response, status: http.StatusOK})
	id := "596069db4bf5"
	image, err := client.CommitContainer(CommitContainerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if image.ID != id {
		t.Errorf("CommitContainer: Wrong image id. Want %q. Got %q.", id, image.ID)
	}
}

func TestCommitContainerParams(t *testing.T) {
	t.Parallel()
	cfg := Config{Memory: 67108864}
	json, _ := json.Marshal(&cfg)
	tests := []struct {
		input  CommitContainerOptions
		params map[string][]string
		body   []byte
	}{
		{CommitContainerOptions{}, map[string][]string{}, nil},
		{CommitContainerOptions{Container: "44c004db4b17"}, map[string][]string{"container": {"44c004db4b17"}}, nil},
		{
			CommitContainerOptions{Container: "44c004db4b17", Repository: "tsuru/python", Message: "something"},
			map[string][]string{"container": {"44c004db4b17"}, "repo": {"tsuru/python"}, "comment": {"something"}},
			nil,
		},
		{
			CommitContainerOptions{Container: "44c004db4b17", Run: &cfg},
			map[string][]string{"container": {"44c004db4b17"}},
			json,
		},
	}
	const expectedPath = "/commit"
	for _, tt := range tests {
		test := tt
		t.Run("", func(t *testing.T) {
			t.Parallel()
			fakeRT := &FakeRoundTripper{message: "{}", status: http.StatusOK}
			client := newTestClient(fakeRT)
			if _, err := client.CommitContainer(test.input); err != nil {
				t.Error(err)
			}
			got := map[string][]string(fakeRT.requests[0].URL.Query())
			if !reflect.DeepEqual(got, test.params) {
				t.Errorf("Expected %#v, got %#v.", test.params, got)
			}
			if path := fakeRT.requests[0].URL.Path; path != expectedPath {
				t.Errorf("Wrong path on request. Want %q. Got %q.", expectedPath, path)
			}
			if meth := fakeRT.requests[0].Method; meth != "POST" {
				t.Errorf("Wrong HTTP method. Want POST. Got %s.", meth)
			}
			if test.body != nil {
				if requestBody, err := ioutil.ReadAll(fakeRT.requests[0].Body); err == nil {
					if !bytes.Equal(requestBody, test.body) {
						t.Errorf("Expected body %#v, got %#v", test.body, requestBody)
					}
				} else {
					t.Errorf("Error reading request body: %#v", err)
				}
			}
		})
	}
}

func TestCommitContainerFailure(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusInternalServerError})
	_, err := client.CommitContainer(CommitContainerOptions{})
	if err == nil {
		t.Error("Expected non-nil error, got <nil>.")
	}
}

func TestCommitContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	_, err := client.CommitContainer(CommitContainerOptions{})
	expected := &NoSuchContainer{ID: ""}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("CommitContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestAttachToContainerLogs(t *testing.T) {
	t.Parallel()
	var req http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{1, 0, 0, 0, 0, 0, 0, 19})
		w.Write([]byte("something happened!"))
		req = *r
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var buf bytes.Buffer
	opts := AttachToContainerOptions{
		Container:    "a123456",
		OutputStream: &buf,
		Stdout:       true,
		Stderr:       true,
		Logs:         true,
	}
	err := client.AttachToContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	expected := "something happened!"
	if buf.String() != expected {
		t.Errorf("AttachToContainer for logs: wrong output. Want %q. Got %q.", expected, buf.String())
	}
	if req.Method != "POST" {
		t.Errorf("AttachToContainer: wrong HTTP method. Want POST. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/containers/a123456/attach"))
	if req.URL.Path != u.Path {
		t.Errorf("AttachToContainer for logs: wrong HTTP path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
	expectedQs := map[string][]string{
		"logs":   {"1"},
		"stdout": {"1"},
		"stderr": {"1"},
	}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expectedQs) {
		t.Errorf("AttachToContainer: wrong query string. Want %#v. Got %#v.", expectedQs, got)
	}
}

func TestAttachToContainer(t *testing.T) {
	t.Parallel()
	reader := strings.NewReader("send value")
	var req http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{1, 0, 0, 0, 0, 0, 0, 5})
		w.Write([]byte("hello"))
		req = *r
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var stdout, stderr bytes.Buffer
	opts := AttachToContainerOptions{
		Container:    "a123456",
		OutputStream: &stdout,
		ErrorStream:  &stderr,
		InputStream:  reader,
		Stdin:        true,
		Stdout:       true,
		Stderr:       true,
		Stream:       true,
		RawTerminal:  true,
	}
	err := client.AttachToContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{
		"stdin":  {"1"},
		"stdout": {"1"},
		"stderr": {"1"},
		"stream": {"1"},
	}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("AttachToContainer: wrong query string. Want %#v. Got %#v.", expected, got)
	}
}

func TestAttachToContainerSentinel(t *testing.T) {
	t.Parallel()
	reader := strings.NewReader("send value")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{1, 0, 0, 0, 0, 0, 0, 5})
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var stdout, stderr bytes.Buffer
	success := make(chan struct{})
	opts := AttachToContainerOptions{
		Container:    "a123456",
		OutputStream: &stdout,
		ErrorStream:  &stderr,
		InputStream:  reader,
		Stdin:        true,
		Stdout:       true,
		Stderr:       true,
		Stream:       true,
		RawTerminal:  true,
		Success:      success,
	}
	errCh := make(chan error)
	go func() {
		errCh <- client.AttachToContainer(opts)
	}()
	success <- <-success
	if err := <-errCh; err != nil {
		t.Error(err)
	}
}

func TestAttachToContainerNilStdout(t *testing.T) {
	t.Parallel()
	reader := strings.NewReader("send value")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{1, 0, 0, 0, 0, 0, 0, 5})
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var stderr bytes.Buffer
	opts := AttachToContainerOptions{
		Container:    "a123456",
		OutputStream: nil,
		ErrorStream:  &stderr,
		InputStream:  reader,
		Stdin:        true,
		Stdout:       true,
		Stderr:       true,
		Stream:       true,
		RawTerminal:  true,
	}
	err := client.AttachToContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAttachToContainerNilStderr(t *testing.T) {
	t.Parallel()
	reader := strings.NewReader("send value")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{1, 0, 0, 0, 0, 0, 0, 5})
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var stdout bytes.Buffer
	opts := AttachToContainerOptions{
		Container:    "a123456",
		OutputStream: &stdout,
		InputStream:  reader,
		Stdin:        true,
		Stdout:       true,
		Stderr:       true,
		Stream:       true,
		RawTerminal:  true,
	}
	err := client.AttachToContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAttachToContainerStdinOnly(t *testing.T) {
	t.Parallel()
	reader := strings.NewReader("send value")
	serverFinished := make(chan struct{})
	clientFinished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("cannot hijack server connection")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		// wait for client to indicate it's finished
		<-clientFinished
		// inform test that the server has finished
		close(serverFinished)
		conn.Close()
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	success := make(chan struct{})
	opts := AttachToContainerOptions{
		Container:   "a123456",
		InputStream: reader,
		Stdin:       true,
		Stdout:      false,
		Stderr:      false,
		Stream:      true,
		RawTerminal: false,
		Success:     success,
	}
	go func() {
		if err := client.AttachToContainer(opts); err != nil {
			t.Error(err)
		}
		// client's attach session is over
		close(clientFinished)
	}()
	success <- <-success
	// wait for server to finish handling attach
	<-serverFinished
}

func TestAttachToContainerRawTerminalFalse(t *testing.T) {
	t.Parallel()
	input := strings.NewReader("send value")
	var req http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req = *r
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("cannot hijack server connection")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		conn.Write([]byte{1, 0, 0, 0, 0, 0, 0, 5})
		conn.Write([]byte("hello"))
		conn.Write([]byte{2, 0, 0, 0, 0, 0, 0, 6})
		conn.Write([]byte("hello!"))
		time.Sleep(10 * time.Millisecond)
		conn.Close()
	}))
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var stdout, stderr bytes.Buffer
	opts := AttachToContainerOptions{
		Container:    "a123456",
		OutputStream: &stdout,
		ErrorStream:  &stderr,
		InputStream:  input,
		Stdin:        true,
		Stdout:       true,
		Stderr:       true,
		Stream:       true,
		RawTerminal:  false,
	}
	client.AttachToContainer(opts)
	expected := map[string][]string{
		"stdin":  {"1"},
		"stdout": {"1"},
		"stderr": {"1"},
		"stream": {"1"},
	}
	got := map[string][]string(req.URL.Query())
	server.Close()
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("AttachToContainer: wrong query string. Want %#v. Got %#v.", expected, got)
	}
	if stdout.String() != "hello" {
		t.Errorf("AttachToContainer: wrong content written to stdout. Want %q. Got %q.", "hello", stdout.String())
	}
	if stderr.String() != "hello!" {
		t.Errorf("AttachToContainer: wrong content written to stderr. Want %q. Got %q.", "hello!", stderr.String())
	}
}

func TestAttachToContainerWithoutContainer(t *testing.T) {
	t.Parallel()
	var client Client
	err := client.AttachToContainer(AttachToContainerOptions{})
	expected := &NoSuchContainer{ID: ""}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("AttachToContainer: wrong error. Want %#v. Got %#v.", expected, err)
	}
}

func TestLogs(t *testing.T) {
	t.Parallel()
	var req http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := []byte{1, 0, 0, 0, 0, 0, 0, 19}
		w.Write(prefix)
		w.Write([]byte("something happened!"))
		req = *r
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var buf bytes.Buffer
	opts := LogsOptions{
		Container:    "a123456",
		OutputStream: &buf,
		Follow:       true,
		Stdout:       true,
		Stderr:       true,
		Timestamps:   true,
	}
	err := client.Logs(opts)
	if err != nil {
		t.Fatal(err)
	}
	expected := "something happened!"
	if buf.String() != expected {
		t.Errorf("Logs: wrong output. Want %q. Got %q.", expected, buf.String())
	}
	if req.Method != "GET" {
		t.Errorf("Logs: wrong HTTP method. Want GET. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/containers/a123456/logs"))
	if req.URL.Path != u.Path {
		t.Errorf("AttachToContainer for logs: wrong HTTP path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
	expectedQs := map[string][]string{
		"follow":     {"1"},
		"stdout":     {"1"},
		"stderr":     {"1"},
		"timestamps": {"1"},
		"tail":       {"all"},
	}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expectedQs) {
		t.Errorf("Logs: wrong query string. Want %#v. Got %#v.", expectedQs, got)
	}
}

func TestLogsNilStdoutDoesntFail(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := []byte{1, 0, 0, 0, 0, 0, 0, 19}
		w.Write(prefix)
		w.Write([]byte("something happened!"))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	opts := LogsOptions{
		Container:  "a123456",
		Follow:     true,
		Stdout:     true,
		Stderr:     true,
		Timestamps: true,
	}
	err := client.Logs(opts)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLogsNilStderrDoesntFail(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := []byte{2, 0, 0, 0, 0, 0, 0, 19}
		w.Write(prefix)
		w.Write([]byte("something happened!"))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	opts := LogsOptions{
		Container:  "a123456",
		Follow:     true,
		Stdout:     true,
		Stderr:     true,
		Timestamps: true,
	}
	err := client.Logs(opts)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLogsSpecifyingTail(t *testing.T) {
	t.Parallel()
	var req http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := []byte{1, 0, 0, 0, 0, 0, 0, 19}
		w.Write(prefix)
		w.Write([]byte("something happened!"))
		req = *r
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var buf bytes.Buffer
	opts := LogsOptions{
		Container:    "a123456",
		OutputStream: &buf,
		Follow:       true,
		Stdout:       true,
		Stderr:       true,
		Timestamps:   true,
		Tail:         "100",
	}
	err := client.Logs(opts)
	if err != nil {
		t.Fatal(err)
	}
	expected := "something happened!"
	if buf.String() != expected {
		t.Errorf("Logs: wrong output. Want %q. Got %q.", expected, buf.String())
	}
	if req.Method != "GET" {
		t.Errorf("Logs: wrong HTTP method. Want GET. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/containers/a123456/logs"))
	if req.URL.Path != u.Path {
		t.Errorf("AttachToContainer for logs: wrong HTTP path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
	expectedQs := map[string][]string{
		"follow":     {"1"},
		"stdout":     {"1"},
		"stderr":     {"1"},
		"timestamps": {"1"},
		"tail":       {"100"},
	}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expectedQs) {
		t.Errorf("Logs: wrong query string. Want %#v. Got %#v.", expectedQs, got)
	}
}

func TestLogsRawTerminal(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("something happened!"))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	var buf bytes.Buffer
	opts := LogsOptions{
		Container:    "a123456",
		OutputStream: &buf,
		Follow:       true,
		RawTerminal:  true,
		Stdout:       true,
		Stderr:       true,
		Timestamps:   true,
		Tail:         "100",
	}
	err := client.Logs(opts)
	if err != nil {
		t.Fatal(err)
	}
	expected := "something happened!"
	if buf.String() != expected {
		t.Errorf("Logs: wrong output. Want %q. Got %q.", expected, buf.String())
	}
}

func TestLogsNoContainer(t *testing.T) {
	t.Parallel()
	var client Client
	err := client.Logs(LogsOptions{})
	expected := &NoSuchContainer{ID: ""}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("AttachToContainer: wrong error. Want %#v. Got %#v.", expected, err)
	}
}

func TestNoSuchContainerError(t *testing.T) {
	t.Parallel()
	err := &NoSuchContainer{ID: "i345"}
	expected := "No such container: i345"
	if got := err.Error(); got != expected {
		t.Errorf("NoSuchContainer: wrong message. Want %q. Got %q.", expected, got)
	}
}

func TestNoSuchContainerErrorMessage(t *testing.T) {
	t.Parallel()
	err := &NoSuchContainer{ID: "i345", Err: errors.New("some advanced error info")}
	expected := "some advanced error info"
	if got := err.Error(); got != expected {
		t.Errorf("NoSuchContainer: wrong message. Want %q. Got %q.", expected, got)
	}
}

func TestExportContainer(t *testing.T) {
	t.Parallel()
	content := "exported container tar content"
	out := stdoutMock{bytes.NewBufferString(content)}
	client := newTestClient(&FakeRoundTripper{status: http.StatusOK})
	opts := ExportContainerOptions{ID: "4fa6e0f0c678", OutputStream: out}
	err := client.ExportContainer(opts)
	if err != nil {
		t.Errorf("ExportContainer: caugh error %#v while exporting container, expected nil", err.Error())
	}
	if out.String() != content {
		t.Errorf("ExportContainer: wrong stdout. Want %#v. Got %#v.", content, out.String())
	}
}

func TestExportContainerNoId(t *testing.T) {
	t.Parallel()
	client := Client{}
	out := stdoutMock{bytes.NewBufferString("")}
	err := client.ExportContainer(ExportContainerOptions{OutputStream: out})
	e, ok := err.(*NoSuchContainer)
	if !ok {
		t.Errorf("ExportContainer: wrong error. Want NoSuchContainer. Got %#v.", e)
	}
	if e.ID != "" {
		t.Errorf("ExportContainer: wrong ID. Want %q. Got %q", "", e.ID)
	}
}

func TestUploadToContainer(t *testing.T) {
	t.Parallel()
	content := "File content"
	in := stdinMock{bytes.NewBufferString(content)}
	fakeRT := &FakeRoundTripper{status: http.StatusOK}
	client := newTestClient(fakeRT)
	opts := UploadToContainerOptions{
		Path:        "abc",
		InputStream: in,
	}
	err := client.UploadToContainer("a123456", opts)
	if err != nil {
		t.Errorf("UploadToContainer: caught error %#v while uploading archive to container, expected nil", err)
	}

	req := fakeRT.requests[0]

	if req.Method != "PUT" {
		t.Errorf("UploadToContainer{Path:abc}: Wrong HTTP method.  Want PUT. Got %s", req.Method)
	}

	if pathParam := req.URL.Query().Get("path"); pathParam != "abc" {
		t.Errorf("ListImages({Path:abc}): Wrong parameter. Want path=abc.  Got path=%s", pathParam)
	}
}

func TestDownloadFromContainer(t *testing.T) {
	t.Parallel()
	filecontent := "File content"
	client := newTestClient(&FakeRoundTripper{message: filecontent, status: http.StatusOK})

	var out bytes.Buffer
	opts := DownloadFromContainerOptions{
		OutputStream: &out,
	}
	err := client.DownloadFromContainer("a123456", opts)
	if err != nil {
		t.Errorf("DownloadFromContainer: caught error %#v while downloading from container, expected nil", err.Error())
	}
	if out.String() != filecontent {
		t.Errorf("DownloadFromContainer: wrong stdout. Want %#v. Got %#v.", filecontent, out.String())
	}
}

func TestCopyFromContainer(t *testing.T) {
	t.Parallel()
	content := "File content"
	out := stdoutMock{bytes.NewBufferString(content)}
	client := newTestClient(&FakeRoundTripper{status: http.StatusOK})
	opts := CopyFromContainerOptions{
		Container:    "a123456",
		OutputStream: &out,
	}
	err := client.CopyFromContainer(opts)
	if err != nil {
		t.Errorf("CopyFromContainer: caught error %#v while copying from container, expected nil", err.Error())
	}
	if out.String() != content {
		t.Errorf("CopyFromContainer: wrong stdout. Want %#v. Got %#v.", content, out.String())
	}
}

func TestCopyFromContainerEmptyContainer(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{status: http.StatusOK})
	err := client.CopyFromContainer(CopyFromContainerOptions{})
	_, ok := err.(*NoSuchContainer)
	if !ok {
		t.Errorf("CopyFromContainer: invalid error returned. Want NoSuchContainer, got %#v.", err)
	}
}

func TestCopyFromContainerDockerAPI124(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{status: http.StatusOK})
	client.serverAPIVersion = apiVersion124
	opts := CopyFromContainerOptions{
		Container: "a123456",
	}
	err := client.CopyFromContainer(opts)
	if err == nil {
		t.Fatal("got unexpected <nil> error")
	}
	expectedMsg := "go-dockerclient: CopyFromContainer is no longer available in Docker >= 1.12, use DownloadFromContainer instead"
	if err.Error() != expectedMsg {
		t.Errorf("wrong error message\nWant %q\nGot  %q", expectedMsg, err.Error())
	}
}

func TestPassingNameOptToCreateContainerReturnsItInContainer(t *testing.T) {
	t.Parallel()
	jsonContainer := `{
             "Id": "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2",
	     "Warnings": []
}`
	fakeRT := &FakeRoundTripper{message: jsonContainer, status: http.StatusOK}
	client := newTestClient(fakeRT)
	config := Config{AttachStdout: true, AttachStdin: true}
	opts := CreateContainerOptions{Name: "TestCreateContainer", Config: &config}
	container, err := client.CreateContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	if container.Name != "TestCreateContainer" {
		t.Errorf("Container name expected to be TestCreateContainer, was %s", container.Name)
	}
}

func TestAlwaysRestart(t *testing.T) {
	t.Parallel()
	policy := AlwaysRestart()
	if policy.Name != "always" {
		t.Errorf("AlwaysRestart(): wrong policy name. Want %q. Got %q", "always", policy.Name)
	}
	if policy.MaximumRetryCount != 0 {
		t.Errorf("AlwaysRestart(): wrong MaximumRetryCount. Want 0. Got %d", policy.MaximumRetryCount)
	}
}

func TestRestartOnFailure(t *testing.T) {
	t.Parallel()
	const retry = 5
	policy := RestartOnFailure(retry)
	if policy.Name != "on-failure" {
		t.Errorf("RestartOnFailure(%d): wrong policy name. Want %q. Got %q", retry, "on-failure", policy.Name)
	}
	if policy.MaximumRetryCount != retry {
		t.Errorf("RestartOnFailure(%d): wrong MaximumRetryCount. Want %d. Got %d", retry, retry, policy.MaximumRetryCount)
	}
}

func TestRestartUnlessStopped(t *testing.T) {
	t.Parallel()
	policy := RestartUnlessStopped()
	if policy.Name != "unless-stopped" {
		t.Errorf("RestartUnlessStopped(): wrong policy name. Want %q. Got %q", "unless-stopped", policy.Name)
	}
	if policy.MaximumRetryCount != 0 {
		t.Errorf("RestartUnlessStopped(): wrong MaximumRetryCount. Want 0. Got %d", policy.MaximumRetryCount)
	}
}

func TestNeverRestart(t *testing.T) {
	t.Parallel()
	policy := NeverRestart()
	if policy.Name != "no" {
		t.Errorf("NeverRestart(): wrong policy name. Want %q. Got %q", "always", policy.Name)
	}
	if policy.MaximumRetryCount != 0 {
		t.Errorf("NeverRestart(): wrong MaximumRetryCount. Want 0. Got %d", policy.MaximumRetryCount)
	}
}

func TestTopContainer(t *testing.T) {
	t.Parallel()
	jsonTop := `{
  "Processes": [
    [
      "ubuntu",
      "3087",
      "815",
      "0",
      "01:44",
      "?",
      "00:00:00",
      "cmd1"
    ],
    [
      "root",
      "3158",
      "3087",
      "0",
      "01:44",
      "?",
      "00:00:01",
      "cmd2"
    ]
  ],
  "Titles": [
    "UID",
    "PID",
    "PPID",
    "C",
    "STIME",
    "TTY",
    "TIME",
    "CMD"
  ]
}`
	var expected TopResult
	err := json.Unmarshal([]byte(jsonTop), &expected)
	if err != nil {
		t.Fatal(err)
	}
	id := "4fa6e0f0"
	fakeRT := &FakeRoundTripper{message: jsonTop, status: http.StatusOK}
	client := newTestClient(fakeRT)
	processes, err := client.TopContainer(id, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(processes, expected) {
		t.Errorf("TopContainer: Expected %#v. Got %#v.", expected, processes)
	}
	if len(processes.Processes) != 2 || len(processes.Processes[0]) != 8 ||
		processes.Processes[0][7] != "cmd1" {
		t.Errorf("TopContainer: Process list to include cmd1. Got %#v.", processes)
	}
	expectedURI := "/containers/" + id + "/top"
	if !strings.HasSuffix(fakeRT.requests[0].URL.String(), expectedURI) {
		t.Errorf("TopContainer: Expected URI to have %q. Got %q.", expectedURI, fakeRT.requests[0].URL.String())
	}
}

func TestTopContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	_, err := client.TopContainer("abef348", "")
	expected := &NoSuchContainer{ID: "abef348"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("StopContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestTopContainerWithPsArgs(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "no such container", status: http.StatusNotFound}
	client := newTestClient(fakeRT)
	expectedErr := &NoSuchContainer{ID: "abef348"}
	if _, err := client.TopContainer("abef348", "aux"); !reflect.DeepEqual(expectedErr, err) {
		t.Errorf("TopContainer: Expected %v. Got %v.", expectedErr, err)
	}
	expectedURI := "/containers/abef348/top?ps_args=aux"
	if !strings.HasSuffix(fakeRT.requests[0].URL.String(), expectedURI) {
		t.Errorf("TopContainer: Expected URI to have %q. Got %q.", expectedURI, fakeRT.requests[0].URL.String())
	}
}

func TestStats(t *testing.T) {
	t.Parallel()
	jsonStats1 := `{
       "read" : "2015-01-08T22:57:31.547920715Z",
       "network" : {
          "rx_dropped" : 0,
          "rx_bytes" : 648,
          "rx_errors" : 0,
          "tx_packets" : 8,
          "tx_dropped" : 0,
          "rx_packets" : 8,
          "tx_errors" : 0,
          "tx_bytes" : 648
       },
	   "networks" : {
		   "eth0":{
			   "rx_dropped" : 0,
			   "rx_bytes" : 648,
			   "rx_errors" : 0,
			   "tx_packets" : 8,
			   "tx_dropped" : 0,
			   "rx_packets" : 8,
			   "tx_errors" : 0,
			   "tx_bytes" : 648
		   }
	   },
       "memory_stats" : {
          "stats" : {
             "total_pgmajfault" : 0,
             "cache" : 0,
             "mapped_file" : 0,
             "total_inactive_file" : 0,
             "pgpgout" : 414,
             "rss" : 6537216,
             "total_mapped_file" : 0,
             "writeback" : 0,
             "unevictable" : 0,
             "pgpgin" : 477,
             "total_unevictable" : 0,
             "pgmajfault" : 0,
             "total_rss" : 6537216,
             "total_rss_huge" : 6291456,
             "total_writeback" : 0,
             "total_inactive_anon" : 0,
             "rss_huge" : 6291456,
	     "hierarchical_memory_limit": 189204833,
             "total_pgfault" : 964,
             "total_active_file" : 0,
             "active_anon" : 6537216,
             "total_active_anon" : 6537216,
             "total_pgpgout" : 414,
             "total_cache" : 0,
             "inactive_anon" : 0,
             "active_file" : 0,
             "pgfault" : 964,
             "inactive_file" : 0,
             "total_pgpgin" : 477,
             "swap" : 47312896,
             "hierarchical_memsw_limit" : 1610612736
          },
          "max_usage" : 6651904,
          "usage" : 6537216,
          "failcnt" : 0,
          "limit" : 67108864
       },
       "blkio_stats": {
          "io_service_bytes_recursive": [
             {
                "major": 8,
                "minor": 0,
                "op": "Read",
                "value": 428795731968
             },
             {
                "major": 8,
                "minor": 0,
                "op": "Write",
                "value": 388177920
             }
          ],
          "io_serviced_recursive": [
             {
                "major": 8,
                "minor": 0,
                "op": "Read",
                "value": 25994442
             },
             {
                "major": 8,
                "minor": 0,
                "op": "Write",
                "value": 1734
             }
          ],
          "io_queue_recursive": [],
          "io_service_time_recursive": [],
          "io_wait_time_recursive": [],
          "io_merged_recursive": [],
          "io_time_recursive": [],
          "sectors_recursive": []
       },
       "cpu_stats" : {
          "cpu_usage" : {
             "percpu_usage" : [
                16970827,
                1839451,
                7107380,
                10571290
             ],
             "usage_in_usermode" : 10000000,
             "total_usage" : 36488948,
             "usage_in_kernelmode" : 20000000
          },
          "system_cpu_usage" : 20091722000000000,
		  "online_cpus": 4
       },
       "precpu_stats" : {
          "cpu_usage" : {
             "percpu_usage" : [
                16970827,
                1839451,
                7107380,
                10571290
             ],
             "usage_in_usermode" : 10000000,
             "total_usage" : 36488948,
             "usage_in_kernelmode" : 20000000
          },
          "system_cpu_usage" : 20091722000000000,
		  "online_cpus": 4
       }
    }`
	// 1 second later, cache is 100
	jsonStats2 := `{
       "read" : "2015-01-08T22:57:32.547920715Z",
	   "networks" : {
		   "eth0":{
			   "rx_dropped" : 0,
			   "rx_bytes" : 648,
			   "rx_errors" : 0,
			   "tx_packets" : 8,
			   "tx_dropped" : 0,
			   "rx_packets" : 8,
			   "tx_errors" : 0,
			   "tx_bytes" : 648
		   }
	   },
	   "memory_stats" : {
          "stats" : {
             "total_pgmajfault" : 0,
             "cache" : 100,
             "mapped_file" : 0,
             "total_inactive_file" : 0,
             "pgpgout" : 414,
             "rss" : 6537216,
             "total_mapped_file" : 0,
             "writeback" : 0,
             "unevictable" : 0,
             "pgpgin" : 477,
             "total_unevictable" : 0,
             "pgmajfault" : 0,
             "total_rss" : 6537216,
             "total_rss_huge" : 6291456,
             "total_writeback" : 0,
             "total_inactive_anon" : 0,
             "rss_huge" : 6291456,
             "total_pgfault" : 964,
             "total_active_file" : 0,
             "active_anon" : 6537216,
             "total_active_anon" : 6537216,
             "total_pgpgout" : 414,
             "total_cache" : 0,
             "inactive_anon" : 0,
             "active_file" : 0,
             "pgfault" : 964,
             "inactive_file" : 0,
             "total_pgpgin" : 477,
             "swap" : 47312896,
             "hierarchical_memsw_limit" : 1610612736
          },
          "max_usage" : 6651904,
          "usage" : 6537216,
          "failcnt" : 0,
          "limit" : 67108864
       },
       "blkio_stats": {
          "io_service_bytes_recursive": [
             {
                "major": 8,
                "minor": 0,
                "op": "Read",
                "value": 428795731968
             },
             {
                "major": 8,
                "minor": 0,
                "op": "Write",
                "value": 388177920
             }
          ],
          "io_serviced_recursive": [
             {
                "major": 8,
                "minor": 0,
                "op": "Read",
                "value": 25994442
             },
             {
                "major": 8,
                "minor": 0,
                "op": "Write",
                "value": 1734
             }
          ],
          "io_queue_recursive": [],
          "io_service_time_recursive": [],
          "io_wait_time_recursive": [],
          "io_merged_recursive": [],
          "io_time_recursive": [],
          "sectors_recursive": []
       },
       "cpu_stats" : {
          "cpu_usage" : {
             "percpu_usage" : [
                16970827,
                1839451,
                7107380,
                10571290
             ],
             "usage_in_usermode" : 10000000,
             "total_usage" : 36488948,
             "usage_in_kernelmode" : 20000000
          },
          "system_cpu_usage" : 20091722000000000,
		  "online_cpus": 4
       },
       "precpu_stats" : {
          "cpu_usage" : {
             "percpu_usage" : [
                16970827,
                1839451,
                7107380,
                10571290
             ],
             "usage_in_usermode" : 10000000,
             "total_usage" : 36488948,
             "usage_in_kernelmode" : 20000000
          },
          "system_cpu_usage" : 20091722000000000,
		  "online_cpus": 4
       }
    }`
	var expected1 Stats
	var expected2 Stats
	err := json.Unmarshal([]byte(jsonStats1), &expected1)
	if err != nil {
		t.Fatal(err)
	}
	err = json.Unmarshal([]byte(jsonStats2), &expected2)
	if err != nil {
		t.Fatal(err)
	}
	id := "4fa6e0f0"

	var req http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jsonStats1))
		w.Write([]byte(jsonStats2))
		req = *r
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	client.SkipServerVersionCheck = true
	errC := make(chan error, 1)
	statsC := make(chan *Stats)
	done := make(chan bool)
	defer close(done)
	go func() {
		errC <- client.Stats(StatsOptions{ID: id, Stats: statsC, Stream: true, Done: done})
		close(errC)
	}()
	var resultStats []*Stats
	for {
		stats, ok := <-statsC
		if !ok {
			break
		}
		resultStats = append(resultStats, stats)
	}
	err = <-errC
	if err != nil {
		t.Fatal(err)
	}
	if len(resultStats) != 2 {
		t.Fatalf("Stats: Expected 2 results. Got %d.", len(resultStats))
	}
	if !reflect.DeepEqual(resultStats[0], &expected1) {
		t.Errorf("Stats: Expected:\n%+v\nGot:\n%+v", expected1, resultStats[0])
	}
	if !reflect.DeepEqual(resultStats[1], &expected2) {
		t.Errorf("Stats: Expected:\n%+v\nGot:\n%+v", expected2, resultStats[1])
	}
	if req.Method != "GET" {
		t.Errorf("Stats: wrong HTTP method. Want GET. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/containers/" + id + "/stats"))
	if req.URL.Path != u.Path {
		t.Errorf("Stats: wrong HTTP path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
}

func TestStatsContainerNotFound(t *testing.T) {
	t.Parallel()
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	statsC := make(chan *Stats)
	done := make(chan bool)
	defer close(done)
	err := client.Stats(StatsOptions{ID: "abef348", Stats: statsC, Stream: true, Done: done})
	expected := &NoSuchContainer{ID: "abef348"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("Stats: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestRenameContainer(t *testing.T) {
	t.Parallel()
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	opts := RenameContainerOptions{ID: "something_old", Name: "something_new"}
	err := client.RenameContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("RenameContainer: wrong HTTP method. Want %q. Got %q.", "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/something_old/rename?name=something_new"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("RenameContainer: Wrong path in request. Want %q. Got %q.", expectedURL.Path, gotPath)
	}
	expectedValues := expectedURL.Query()["name"]
	actualValues := req.URL.Query()["name"]
	if len(actualValues) != 1 || expectedValues[0] != actualValues[0] {
		t.Errorf("RenameContainer: Wrong params in request. Want %q. Got %q.", expectedValues, actualValues)
	}
}

// sleepyRoundTripper implements the http.RoundTripper interface. It sleeps
// for the 'sleep' duration and then returns an error for RoundTrip method.
type sleepyRoudTripper struct {
	sleepDuration time.Duration
}

func (rt *sleepyRoudTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	time.Sleep(rt.sleepDuration)
	return nil, fmt.Errorf("Can't complete round trip")
}

func TestInspectContainerWhenContextTimesOut(t *testing.T) {
	t.Parallel()
	rt := sleepyRoudTripper{sleepDuration: 200 * time.Millisecond}

	client := newTestClient(&rt)

	ctx, cancel := context.WithTimeout(context.TODO(), 100*time.Millisecond)
	defer cancel()

	_, err := client.InspectContainerWithContext("id", ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected 'DeadlineExceededError', got: %v", err)
	}
}

func TestStartContainerWhenContextTimesOut(t *testing.T) {
	t.Parallel()
	rt := sleepyRoudTripper{sleepDuration: 200 * time.Millisecond}

	client := newTestClient(&rt)

	ctx, cancel := context.WithTimeout(context.TODO(), 100*time.Millisecond)
	defer cancel()

	err := client.StartContainerWithContext("id", nil, ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected 'DeadlineExceededError', got: %v", err)
	}
}

func TestStopContainerWhenContextTimesOut(t *testing.T) {
	t.Parallel()
	rt := sleepyRoudTripper{sleepDuration: 300 * time.Millisecond}

	client := newTestClient(&rt)

	ctx, cancel := context.WithTimeout(context.TODO(), 50*time.Millisecond)
	defer cancel()

	err := client.StopContainerWithContext("id", 10, ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected 'DeadlineExceededError', got: %v", err)
	}
}

func TestWaitContainerWhenContextTimesOut(t *testing.T) {
	t.Parallel()
	rt := sleepyRoudTripper{sleepDuration: 200 * time.Millisecond}

	client := newTestClient(&rt)

	ctx, cancel := context.WithTimeout(context.TODO(), 100*time.Millisecond)
	defer cancel()

	_, err := client.WaitContainerWithContext("id", ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected 'DeadlineExceededError', got: %v", err)
	}
}

func TestPruneContainers(t *testing.T) {
	t.Parallel()
	results := `{
		"ContainersDeleted": [
			"a", "b", "c"
		],
		"SpaceReclaimed": 123
	}`

	expected := &PruneContainersResults{}
	err := json.Unmarshal([]byte(results), expected)
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(&FakeRoundTripper{message: results, status: http.StatusOK})
	got, err := client.PruneContainers(PruneContainersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("PruneContainers: Expected %#v. Got %#v.", expected, got)
	}
}
