import os, subprocess, sys, time, urllib.request
URL = "http://echo.openshell-sandboxes.svc.cluster.local:8080/"
def call(who):
    req = urllib.request.Request(URL, headers={"X-Who": who, "Authorization": "Bearer " + os.environ.get("TRIAL_TOKEN", "")})
    try:
        urllib.request.urlopen(req, timeout=5).read()
    except Exception as err:
        print(who, "failed", err, flush=True)
if len(sys.argv) > 1:
    call(sys.argv[1])
    sys.exit(0)
print("placeholder in the environment:", os.environ.get("TRIAL_TOKEN"), flush=True)
i = 0
while True:
    call("long-lived")
    if i % 5 == 0:
        subprocess.run([sys.executable, "/tmp/probe.py", "new-%d" % i])
    i += 1
    time.sleep(1)
