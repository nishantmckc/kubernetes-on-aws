/*
Copyright 2015 The Kubernetes Authors.
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

package e2e

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rgclient "github.com/szuecs/routegroup-client"
	rgv1 "github.com/szuecs/routegroup-client/apis/zalando.org/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/test/e2e/framework"
)

var _ = framework.KubeDescribe("RouteGroup ALB creation", func() {
	f := framework.NewDefaultFramework("routegroup")
	var (
		cs rgclient.Interface
	)
	BeforeEach(func() {
		By("Creating an rgclient Clientset")
		config, err := framework.LoadConfig()
		Expect(err).NotTo(HaveOccurred())
		config.QPS = f.Options.ClientQPS
		config.Burst = f.Options.ClientBurst
		if f.Options.GroupVersion != nil {
			config.GroupVersion = f.Options.GroupVersion
		}
		cs, err = rgclient.NewClientset(config)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Should create valid https and http ALB endpoint [RouteGroup] [Zalando]", func() {
		serviceName := "rg-test"
		nameprefix := serviceName + "-"
		ns := f.Namespace.Name
		hostName := fmt.Sprintf("%s-%d.%s", serviceName, time.Now().UTC().Unix(), E2EHostedZone())
		labels := map[string]string{
			"app": serviceName,
		}
		port := 83
		targetPort := 80

		// SVC
		By("Creating service " + serviceName + " in namespace " + ns)
		service := createServiceTypeClusterIP(serviceName, labels, port, targetPort)
		defer func() {
			By("deleting the service")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Services(ns).Delete(service.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()
		_, err := cs.CoreV1().Services(ns).Create(service)
		Expect(err).NotTo(HaveOccurred())

		// POD
		By("Creating a POD with prefix " + nameprefix + " in namespace " + ns)
		expectedResponse := "OK RG1"
		pod := createSkipperPod(nameprefix, ns, fmt.Sprintf(`r0: * -> inlineContent("%s") -> <shunt>`, expectedResponse), labels, targetPort)
		defer func() {
			By("deleting the pod")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Pods(ns).Delete(pod.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()

		_, err = cs.CoreV1().Pods(ns).Create(pod)
		Expect(err).NotTo(HaveOccurred())
		framework.ExpectNoError(f.WaitForPodRunning(pod.Name))

		// RouteGroup
		By("Creating a routegroup with name " + serviceName + " in namespace " + ns + " with hostname " + hostName)
		rg := createRouteGroup(serviceName, hostName, ns, labels, nil, port, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/",
		})
		defer func() {
			By("deleting the routegroup")
			defer GinkgoRecover()
			err2 := cs.ZalandoV1().RouteGroups(ns).Delete(rg.Name, metav1.DeleteOptions{})
			Expect(err2).NotTo(HaveOccurred())
		}()
		rgCreate, err := cs.ZalandoV1().RouteGroups(ns).Create(rg, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		addr, err := waitForRouteGroup(cs, rgCreate.Name, rgCreate.Namespace, 10*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		rgGot, err := cs.ZalandoV1().RouteGroups(ns).Get(rg.Name, metav1.GetOptions{ResourceVersion: "0"})
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("ALB endpoint from routegroup status: %s", rgGot.Status.LoadBalancer.RouteGroup[0].Hostname))

		//  skipper http -> https redirect
		By("Waiting for skipper route to default redirect from http to https, to see that our routegroup-controller and skipper works")
		err = waitForResponse(addr, "http", 10*time.Minute, isRedirect, true)
		Expect(err).NotTo(HaveOccurred())

		// ALB ready
		By("Waiting for ALB to create endpoint " + addr + " and skipper route, to see that our routegroup-controller and skipper works")
		err = waitForResponse(addr, "https", 10*time.Minute, isNotFound, true)
		Expect(err).NotTo(HaveOccurred())

		// DNS ready
		By("Waiting for DNS to see that external-dns and skipper route to service and pod works")
		err = waitForResponse(hostName, "https", 10*time.Minute, isSuccess, false)
		Expect(err).NotTo(HaveOccurred())

		// response is from our backend
		By("checking the response body we know, if we got the response from our backend")
		resp, err := http.Get("https://" + hostName)
		Expect(err).NotTo(HaveOccurred())
		b, err := ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal(expectedResponse))
	})

	It("Should create a route with predicates [RouteGroup] [Zalando]", func() {
		serviceName := "rg-test-pred"
		nameprefix := serviceName + "-"
		ns := f.Namespace.Name
		hostName := fmt.Sprintf("%s-%d.%s", serviceName, time.Now().UTC().Unix(), E2EHostedZone())
		labels := map[string]string{
			"app": serviceName,
		}
		port := 83
		targetPort := 80

		// SVC
		By("Creating service " + serviceName + " in namespace " + ns)
		service := createServiceTypeClusterIP(serviceName, labels, port, targetPort)
		defer func() {
			By("deleting the service")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Services(ns).Delete(service.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()
		_, err := cs.CoreV1().Services(ns).Create(service)
		Expect(err).NotTo(HaveOccurred())

		// POD
		By("Creating a POD with prefix " + nameprefix + " in namespace " + ns)
		expectedResponse := "OK RG predicate"
		pod := createSkipperPod(
			nameprefix,
			ns,
			fmt.Sprintf(`rHealth: Path("/") -> inlineContent("OK") -> <shunt>;
rBackend: Path("/backend") -> inlineContent("%s") -> <shunt>;`,
				expectedResponse),
			labels,
			targetPort)
		defer func() {
			By("deleting the pod")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Pods(ns).Delete(pod.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()

		_, err = cs.CoreV1().Pods(ns).Create(pod)
		Expect(err).NotTo(HaveOccurred())
		framework.ExpectNoError(f.WaitForPodRunning(pod.Name))

		// RouteGroup
		By("Creating a routegroup with name " + serviceName + " in namespace " + ns + " with hostname " + hostName)
		rg := createRouteGroup(serviceName, hostName, ns, labels, nil, port, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/",
		}, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/backend",
			Methods:     []string{"GET"},
			Predicates:  []string{`Header("Foo", "bar")`},
		})
		defer func() {
			By("deleting the routegroup")
			defer GinkgoRecover()
			err2 := cs.ZalandoV1().RouteGroups(ns).Delete(rg.Name, metav1.DeleteOptions{})
			Expect(err2).NotTo(HaveOccurred())
		}()
		rgCreate, err := cs.ZalandoV1().RouteGroups(ns).Create(rg, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = waitForRouteGroup(cs, rgCreate.Name, rgCreate.Namespace, 10*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		rgGot, err := cs.ZalandoV1().RouteGroups(ns).Get(rg.Name, metav1.GetOptions{ResourceVersion: "0"})
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("ALB endpoint from routegroup status: %s", rgGot.Status.LoadBalancer.RouteGroup[0].Hostname))

		// DNS ready
		By("Waiting for ALB, DNS and skipper route to service and pod works")
		err = waitForResponse(hostName, "https", 10*time.Minute, isSuccess, false)
		Expect(err).NotTo(HaveOccurred())

		// response is from our backend
		By("checking the response body we know, if we got the response from our backend")
		resp, err := http.Get("https://" + hostName)
		Expect(err).NotTo(HaveOccurred())
		b, err := ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal("OK"))

		// checking backend route with predicates
		By("checking the response for a request to /backend we know if we got the correct route")
		resp, err = http.Get("https://" + hostName + "/backend")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusNotFound)
		req, err := http.NewRequest("GET", "https://"+hostName+"/backend", nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Foo", "bar")
		resp, err = http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusOK)
		b, err = ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal(expectedResponse))
	})

	It("Should create routes with filters, predicates and shunt backend [RouteGroup] [Zalando]", func() {
		serviceName := "rg-test-fp"
		nameprefix := serviceName + "-"
		ns := f.Namespace.Name
		hostName := fmt.Sprintf("%s-%d.%s", serviceName, time.Now().UTC().Unix(), E2EHostedZone())
		labels := map[string]string{
			"app": serviceName,
		}
		port := 83
		targetPort := 80

		// SVC
		By("Creating service " + serviceName + " in namespace " + ns)
		service := createServiceTypeClusterIP(serviceName, labels, port, targetPort)
		defer func() {
			By("deleting the service")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Services(ns).Delete(service.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()
		_, err := cs.CoreV1().Services(ns).Create(service)
		Expect(err).NotTo(HaveOccurred())

		// POD
		By("Creating a POD with prefix " + nameprefix + " in namespace " + ns)
		expectedResponse := "OK RG fp"
		pod := createSkipperPod(
			nameprefix,
			ns,
			fmt.Sprintf(`rHealth: Path("/") -> inlineContent("OK") -> <shunt>;
rBackend: Path("/backend") -> inlineContent("%s") -> <shunt>;`,
				expectedResponse),
			labels,
			targetPort)
		defer func() {
			By("deleting the pod")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Pods(ns).Delete(pod.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()

		_, err = cs.CoreV1().Pods(ns).Create(pod)
		Expect(err).NotTo(HaveOccurred())
		framework.ExpectNoError(f.WaitForPodRunning(pod.Name))

		// RouteGroup
		By("Creating a routegroup with name " + serviceName + " in namespace " + ns + " with hostname " + hostName)
		rg := createRouteGroup(serviceName, hostName, ns, labels, nil, port, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/",
		}, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/backend",
			Methods:     []string{"GET"},
			Predicates: []string{
				`Header("Foo", "bar")`,
			},
			Filters: []string{
				`status(201)`,
			},
		}, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/no-match",
			Methods:     []string{"GET"},
			Predicates:  []string{`Method("HEAD")`},
		}, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/multi-methods",
			Methods:     []string{"GET", "HEAD"},
		}, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/router-response",
			Filters: []string{
				`status(418) -> inlineContent("I am a teapot")`,
			},
			Backends: []rgv1.RouteGroupBackendReference{
				{
					BackendName: "router",
					Weight:      1,
				},
			},
		})
		defer func() {
			By("deleting the routegroup")
			defer GinkgoRecover()
			err2 := cs.ZalandoV1().RouteGroups(ns).Delete(rg.Name, metav1.DeleteOptions{})
			Expect(err2).NotTo(HaveOccurred())
		}()
		rgCreate, err := cs.ZalandoV1().RouteGroups(ns).Create(rg, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = waitForRouteGroup(cs, rgCreate.Name, rgCreate.Namespace, 10*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		rgGot, err := cs.ZalandoV1().RouteGroups(ns).Get(rg.Name, metav1.GetOptions{ResourceVersion: "0"})
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("ALB endpoint from routegroup status: %s", rgGot.Status.LoadBalancer.RouteGroup[0].Hostname))

		// DNS ready
		By("Waiting for ALB, DNS and skipper route to service and pod works")
		err = waitForResponse(hostName, "https", 10*time.Minute, isSuccess, false)
		Expect(err).NotTo(HaveOccurred())

		// response for / is from our backend
		By("checking the response body we know, if we got the response from our backend")
		resp, err := http.Get("https://" + hostName)
		Expect(err).NotTo(HaveOccurred())
		b, err := ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal("OK"))

		// checking backend route with predicates and filters
		By("checking the response for a request to /backend we know if we got the correct route")
		resp, err = http.Get("https://" + hostName + "/backend")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusNotFound)
		req, err := http.NewRequest("GET", "https://"+hostName+"/backend", nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Foo", "bar")
		resp, err = http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusCreated)
		b, err = ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal(expectedResponse))

		// checking /no-match 2 different expected method Predicates can lead to 404
		resp, err = http.Get("https://" + hostName + "/no-match")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusNotFound)
		resp, err = http.Head("https://" + hostName + "/no-match")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusNotFound)

		// checking /multi-methods matches correctly
		resp, err = http.Get("https://" + hostName + "/multi-methods")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusOK)
		resp, err = http.Head("https://" + hostName + "/multi-methods")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusOK)

		// checking /router-response matches correctly and response with shunted route
		resp, err = http.Get("https://" + hostName + "/router-response")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode, http.StatusTeapot)
		b, err = ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal("I am a teapot"))
	})

	It("Should create blue-green routes [RouteGroup] [Zalando]", func() {
		serviceName := "rg-test-fp"
		nameprefix := serviceName + "-"
		ns := f.Namespace.Name
		hostName := fmt.Sprintf("%s-%d.%s", serviceName, time.Now().UTC().Unix(), E2EHostedZone())
		labels := map[string]string{
			"app": serviceName,
		}
		port := 83
		targetPort := 80

		// SVC
		By("Creating service " + serviceName + " in namespace " + ns)
		service := createServiceTypeClusterIP(serviceName, labels, port, targetPort)
		defer func() {
			By("deleting the service")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Services(ns).Delete(service.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()
		_, err := cs.CoreV1().Services(ns).Create(service)
		Expect(err).NotTo(HaveOccurred())

		// POD
		By("Creating a POD with prefix " + nameprefix + " in namespace " + ns)
		expectedResponse := "OK RG fp"
		pod := createSkipperPod(
			nameprefix,
			ns,
			fmt.Sprintf(`rHealth: Path("/") -> inlineContent("OK") -> <shunt>;
rBackend: Path("/backend") -> inlineContent("%s") -> <shunt>;`,
				expectedResponse),
			labels,
			targetPort)
		defer func() {
			By("deleting the pod")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Pods(ns).Delete(pod.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()

		_, err = cs.CoreV1().Pods(ns).Create(pod)
		Expect(err).NotTo(HaveOccurred())
		framework.ExpectNoError(f.WaitForPodRunning(pod.Name))

		// RouteGroup
		By("Creating a routegroup with name " + serviceName + " in namespace " + ns + " with hostname " + hostName)
		rg := createRouteGroup(serviceName, hostName, ns, labels, nil, port, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/",
		}, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/blue-green",
			Filters: []string{
				`status(201) -> inlineContent("blue")`,
			},
			Backends: []rgv1.RouteGroupBackendReference{
				{
					BackendName: "router",
					Weight:      1,
				},
			},
		}, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/blue-green",
			Filters: []string{
				`status(202) -> inlineContent("green")`,
			},
			Backends: []rgv1.RouteGroupBackendReference{
				{
					BackendName: "router",
					Weight:      1,
				},
			},
		})
		defer func() {
			By("deleting the routegroup")
			defer GinkgoRecover()
			err2 := cs.ZalandoV1().RouteGroups(ns).Delete(rg.Name, metav1.DeleteOptions{})
			Expect(err2).NotTo(HaveOccurred())
		}()
		rgCreate, err := cs.ZalandoV1().RouteGroups(ns).Create(rg, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = waitForRouteGroup(cs, rgCreate.Name, rgCreate.Namespace, 10*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		rgGot, err := cs.ZalandoV1().RouteGroups(ns).Get(rg.Name, metav1.GetOptions{ResourceVersion: "0"})
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("ALB endpoint from routegroup status: %s", rgGot.Status.LoadBalancer.RouteGroup[0].Hostname))

		// DNS ready
		By("Waiting for ALB, DNS and skipper route to service and pod works")
		err = waitForResponse(hostName, "https", 10*time.Minute, isSuccess, false)
		Expect(err).NotTo(HaveOccurred())

		// response for / is from our backend
		By("checking the response body we know, if we got the response from our backend")
		resp, err := http.Get("https://" + hostName)
		Expect(err).NotTo(HaveOccurred())
		b, err := ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal("OK"))

		// checking blue-green routes are ~50/50 match
		By("checking the response for a request to /blue-green we know if we got the correct route")
		resp, err = http.Get("https://" + hostName + "/blue-green")
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Or(Equal(201), Equal(202)))
		req, err := http.NewRequest("GET", "https://"+hostName+"/blue-green", nil)
		Expect(err).NotTo(HaveOccurred())
		cnt := map[int]int{
			201: 0,
			202: 0,
		}
		for i := 0; i < 100; i++ {
			resp, err = http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			cnt[resp.StatusCode]++
		}
		res201 := cnt[201] > 40 && cnt[201] < 60
		res202 := cnt[202] > 40 && cnt[202] < 60
		Expect(res201).To(BeTrue())
		Expect(res202).To(BeTrue())
	})

	It("Should create NLB routegroup [RouteGroup] [Zalando]", func() {
		serviceName := "rg-test-nlb"
		nameprefix := serviceName + "-"
		ns := f.Namespace.Name
		hostName := fmt.Sprintf("%s-%d.%s", serviceName, time.Now().UTC().Unix(), E2EHostedZone())
		labels := map[string]string{
			"app": serviceName,
		}
		annotations := map[string]string{
			"zalando.org/aws-load-balancer-type": "nlb",
		}
		port := 83
		targetPort := 80
		// SVC
		By("Creating service " + serviceName + " in namespace " + ns)
		service := createServiceTypeClusterIP(serviceName, labels, port, targetPort)
		defer func() {
			By("deleting the service")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Services(ns).Delete(service.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()
		_, err := cs.CoreV1().Services(ns).Create(service)
		Expect(err).NotTo(HaveOccurred())

		// POD
		By("Creating a POD with prefix " + nameprefix + " in namespace " + ns)
		pod := createSkipperPod(
			nameprefix,
			ns,
			`rHealth: Path("/") -> inlineContent("OK") -> <shunt>`,
			labels,
			targetPort)
		defer func() {
			By("deleting the pod")
			defer GinkgoRecover()
			err2 := cs.CoreV1().Pods(ns).Delete(pod.Name, metav1.NewDeleteOptions(0))
			Expect(err2).NotTo(HaveOccurred())
		}()

		_, err = cs.CoreV1().Pods(ns).Create(pod)
		Expect(err).NotTo(HaveOccurred())
		framework.ExpectNoError(f.WaitForPodRunning(pod.Name))

		// RouteGroup
		By("Creating a routegroup with name " + serviceName + " in namespace " + ns + " with hostname " + hostName)
		rg := createRouteGroup(serviceName, hostName, ns, labels, annotations, port, rgv1.RouteGroupRouteSpec{
			PathSubtree: "/",
		})
		defer func() {
			By("deleting the routegroup")
			defer GinkgoRecover()
			err2 := cs.ZalandoV1().RouteGroups(ns).Delete(rg.Name, metav1.DeleteOptions{})
			Expect(err2).NotTo(HaveOccurred())
		}()
		rgCreate, err := cs.ZalandoV1().RouteGroups(ns).Create(rg, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = waitForRouteGroup(cs, rgCreate.Name, rgCreate.Namespace, 10*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		rgGot, err := cs.ZalandoV1().RouteGroups(ns).Get(rg.Name, metav1.GetOptions{ResourceVersion: "0"})
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("NLB endpoint from routegroup status: %s", rgGot.Status.LoadBalancer.RouteGroup[0].Hostname))

		// DNS ready
		By("Waiting for NLB, DNS and skipper route to service and pod works")
		err = waitForResponse(hostName, "https", 10*time.Minute, isSuccess, false)
		Expect(err).NotTo(HaveOccurred())

		// response for / is from our backend
		By("checking the response body we know, if we got the response from our backend")
		resp, err := http.Get("https://" + hostName)
		Expect(err).NotTo(HaveOccurred())
		b, err := ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal("OK"))
	})
})
