// Command openshell-trial reads an OpenShell gateway for the trial of #118: sandboxes, their policies and the
// advisor's drafts. Reverted with the trial.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	v1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"
)

const timeout = 30 * time.Second

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	c, err := v1.NewClient(v1.Config{Address: os.Getenv("OPENSHELL_ADDR"), Auth: v1.NoAuth()})
	if err != nil {
		return err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ws := "default"
	var out any
	switch args[0] {
	case "list":
		out, err = c.Sandboxes().ListAll(ctx, ws)
	case "draft":
		out, err = c.Policy().GetDraft(ctx, ws, args[1])
	case "policy":
		out, err = c.Policy().ListAll(ctx, ws, args[1])
	case "probe":
		out, err = probe(ctx, c, ws, args[1])
	case "rotate":
		out, err = c.Providers().Update(ctx, ws, &v1.Provider{Name: probeProvider, Type: probeProfile,
			Spec: v1.ProviderSpec{Credentials: map[string]string{"token": args[1]}}})
	case "delete":
		out, err = c.Sandboxes().Delete(ctx, ws, args[1])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

const (
	probeProfile  = "rotor-trial-tool"
	probeProvider = "trial-tool"
	probeEcho     = "echo.openshell-sandboxes.svc.cluster.local"
	probeEchoPort = 8080
	python        = "/opt/python/cpython-3.13.12-linux-x86_64-gnu/bin/python3.13"
)

// probeScript calls the echo service every second from one long-lived process, and every fifth second from a new one.
const probeScript = `
import os, subprocess, sys, time, urllib.request
def call(who):
    req = urllib.request.Request("http://` + probeEcho + `:8080/", headers={"X-Who": who, "Authorization": "Bearer " + os.environ.get("TRIAL_TOKEN", "")})
    try:
        urllib.request.urlopen(req, timeout=5).read()
    except Exception as err:
        print(who, "failed", err, flush=True)
print("placeholder in the environment:", os.environ.get("TRIAL_TOKEN"), flush=True)
if len(sys.argv) > 1:
    call(sys.argv[1]); sys.exit(0)
i = 0
while True:
    call("long-lived")
    if i % 5 == 0:
        subprocess.run([sys.executable, "/tmp/probe.py", "new-%d" % i])
    i += 1
    time.sleep(1)
`

// probe imports a profile for the echo service, makes its provider and starts a sandbox that calls it.
func probe(ctx context.Context, c *v1.Client, ws, token string) (any, error) {
	endpoint := v1.NetworkEndpoint{Host: probeEcho, Port: probeEchoPort}
	imported, err := c.Providers().Profiles().Import(ctx, ws, []v1.ProfileImportItem{{Source: "trial.yaml", Profile: v1.ProviderProfile{
		ID: probeProfile, DisplayName: "Rotor trial tool", Category: v1.ProfileCategoryOther,
		Credentials: []v1.ProfileCredential{{Name: "token", EnvVars: []string{"TRIAL_TOKEN"}, Required: true, Secret: true,
			StablePlaceholder: true}},
		Endpoints: []v1.NetworkEndpoint{endpoint}, Binaries: []v1.NetworkBinary{{Path: python}},
	}}})
	if err != nil {
		return nil, fmt.Errorf("import the profile: %w", err)
	}
	report, _ := json.Marshal(imported)
	fmt.Fprintln(os.Stderr, "import:", string(report))
	if _, err := c.Providers().Ensure(ctx, ws, &v1.Provider{Name: probeProvider, Type: probeProfile,
		Spec: v1.ProviderSpec{Credentials: map[string]string{"token": token}}}); err != nil {
		return nil, fmt.Errorf("make the provider: %w", err)
	}
	script := "open('/tmp/probe.py','w').write(" + fmt.Sprintf("%q", probeScript) + "); exec(open('/tmp/probe.py').read())"
	return c.Sandboxes().Create(ctx, ws, "probe", &v1.SandboxSpec{
		Providers: []string{probeProvider},
		Command:   []string{"/opt/venv/bin/python", "-c", script},
		Template:  &v1.SandboxTemplate{Image: os.Getenv("PROBE_IMAGE")},
		Policy: &v1.SandboxPolicy{Version: 1,
			Filesystem: &v1.FilesystemPolicy{IncludeWorkdir: true, ReadOnly: []string{"/opt"}, ReadWrite: []string{"/tmp"}},
			NetworkPolicies: map[string]v1.NetworkPolicyRule{"echo": {Name: "echo",
				Binaries: []v1.PolicyNetworkBinary{{Path: python}},
				Endpoints: []v1.PolicyNetworkEndpoint{{Host: probeEcho, Port: probeEchoPort, Protocol: "rest",
					Access: v1.NetworkAccessPresetFull, AllowedIPs: []string{"10.96.0.0/12"}}}}}},
	})
}
