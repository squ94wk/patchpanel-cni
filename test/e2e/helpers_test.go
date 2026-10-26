//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	kubeContext = "kind-patchpanel-e2e"
	node        = "patchpanel-e2e-control-plane"
	toolsImage  = "nicolaka/netshoot:v0.14"
	topology    = "lab"
)

var aliasRe = regexp.MustCompile(`alias (\S+)`)

func command(stdin string, name string, args ...string) (string, error) {
	var out bytes.Buffer

	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out.String())
	}

	return strings.TrimSpace(out.String()), nil
}

func kubectlTry(stdin string, args ...string) (string, error) {
	return command(stdin, "kubectl", append([]string{"--context", kubeContext}, args...)...)
}

func kubectl(t *testing.T, args ...string) string {
	t.Helper()

	out, err := kubectlTry("", args...)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func apply(t *testing.T, yaml string) {
	t.Helper()

	_, err := kubectlTry(yaml, "apply", "-f", "-")
	if err != nil {
		t.Fatal(err)
	}
}

// eventually polls fn until it returns nil.
func eventually(t *testing.T, timeout time.Duration, what string, fn func() error) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		err := fn()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %v", what, err)
		}
		time.Sleep(time.Second)
	}
}

// newNamespace creates a namespace with the Topology and the NetworkAttachmentDefinition and
// deletes it afterwards, which waits for all pods and DeviceAssociations to be gone.
func newNamespace(t *testing.T, spec string) string {
	t.Helper()

	ns := "e2e-" + strings.ToLower(strings.TrimPrefix(t.Name(), "Test"))

	kubectl(t, "create", "namespace", ns)
	t.Cleanup(func() {
		if t.Failed() {
			dump(t, ns)
		}

		out, err := kubectlTry("", "delete", "namespace", ns, "--timeout=180s")
		if err != nil {
			t.Errorf("failed to delete namespace: %v: %s", err, out)
		}
	})

	apply(t, fmt.Sprintf(`
apiVersion: patchpanel.squ94wk.github.io/v1alpha1
kind: Topology
metadata: {name: %[2]s, namespace: %[1]s}
spec:
%[3]s
---
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata: {name: %[2]s, namespace: %[1]s}
spec:
  config: '{"cniVersion": "1.0.0", "name": "%[2]s", "type": "patchpanel", "topology": "%[2]s"}'
`, ns, topology, spec))

	return ns
}

func dump(t *testing.T, ns string) {
	t.Helper()

	for _, args := range [][]string{
		{"-n", ns, "get", "pods,deviceassociations", "-o", "wide"},
		{"-n", ns, "get", "events"},
		{"-n", "patchpanel-system", "logs", "ds/patchpanel-agent", "--tail=60"},
	} {
		out, _ := kubectlTry("", args...)
		t.Logf("kubectl %s:\n%s", strings.Join(args, " "), out)
	}

	out, _ := command("", "docker", "exec", node, "ip", "-n", "patchpanel-park", "-d", "link")
	t.Logf("park links:\n%s", out)
}

// startPod creates a pod that claims a device or a group: cniArgs is {"device":"x"} or {"group":"x"}.
func startPod(t *testing.T, ns, name, cniArgs string) {
	t.Helper()

	apply(t, fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %[2]s
  namespace: %[1]s
  annotations:
    k8s.v1.cni.cncf.io/networks: '[{"name": "%[4]s", "cni-args": %[3]s}]'
spec:
  terminationGracePeriodSeconds: 0
  containers:
    - name: main
      image: %[5]s
      imagePullPolicy: IfNotPresent
      command: [sleep, infinity]
      securityContext:
        privileged: true
`, ns, name, cniArgs, topology, toolsImage))
}

func waitReady(t *testing.T, ns, name string) {
	t.Helper()

	kubectl(t, "-n", ns, "wait", "--for=condition=Ready", "pod/"+name, "--timeout=180s")
}

func deletePod(t *testing.T, ns, name string) {
	t.Helper()

	kubectl(t, "-n", ns, "delete", "pod", name, "--timeout=180s")
}

func sh(ns, pod, script string) (string, error) {
	return kubectlTry("", "-n", ns, "exec", pod, "--", "sh", "-c", script)
}

func mustSh(t *testing.T, ns, pod, script string) string {
	t.Helper()

	out, err := sh(ns, pod, script)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func carrier(ns, pod, iface string) (string, error) {
	return sh(ns, pod, "cat /sys/class/net/"+iface+"/carrier")
}

func waitCarrier(t *testing.T, ns, pod, iface, want string) {
	t.Helper()

	eventually(t, 60*time.Second, fmt.Sprintf("carrier %s on %s/%s", want, pod, iface), func() error {
		got, err := carrier(ns, pod, iface)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("carrier is %s", got)
		}

		return nil
	})
}

func waitPing(t *testing.T, ns, pod, ip string) {
	t.Helper()

	eventually(t, 60*time.Second, fmt.Sprintf("ping from %s to %s", pod, ip), func() error {
		_, err := sh(ns, pod, "ping -c 1 -W 1 "+ip)
		return err
	})
}

// stubAliases lists the aliases of the stubs of the namespace in the park netns of the node.
func stubAliases(t *testing.T, ns string) []string {
	t.Helper()

	out, err := command("", "docker", "exec", node, "ip", "-n", "patchpanel-park", "-o", "link", "show")
	if err != nil {
		t.Fatal(err)
	}

	var aliases []string
	for _, m := range aliasRe.FindAllStringSubmatch(out, -1) {
		if strings.HasPrefix(m[1], ns+"/") {
			aliases = append(aliases, m[1])
		}
	}
	sort.Strings(aliases)

	return aliases
}

// holders maps each held device of the namespace to the name of the pod owning its DeviceAssociation.
func holders(t *testing.T, ns string) map[string]string {
	t.Helper()

	out := kubectl(t, "-n", ns, "get", "deviceassociations", "-o",
		`jsonpath={range .items[*]}{.spec.device} {.metadata.ownerReferences[0].name}{"\n"}{end}`)

	held := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			held[fields[0]] = fields[1]
		}
	}

	return held
}

func podEvents(ns, pod string) string {
	out, _ := kubectlTry("", "-n", ns, "get", "events", "--field-selector", "involvedObject.name="+pod,
		"-o", `jsonpath={range .items[*]}{.message}{"\n"}{end}`)

	return out
}
