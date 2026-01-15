# Deployment Instructions (OpenShift)

This controller only clones the repo and notifies an agent. The agent should use these steps to deploy to OpenShift.

1. Inspect the repo for existing deployment artifacts:
   - `openshift/`, `k8s/`, `deploy/`, `helm/`, or `.openshift/`
   - `Dockerfile` or `Containerfile`
   - `README.md` for build/deploy instructions
2. Decide the deployment approach:
   - If OpenShift/Kubernetes manifests exist, apply them with `oc apply -f ...`.
   - If Helm charts exist, deploy with `helm upgrade --install ...`.
   - If only a Dockerfile exists, build and push an image, then update the deployment to use it.
3. Use OpenShift CLI:
   - `oc login <OPENSHIFT_API_URL> --token=<OPENSHIFT_TOKEN>`
   - `oc project <TARGET_NAMESPACE>`
4. Deploy and verify:
   - `oc get pods,svc,route` (or `oc get all`)
   - If routes are created, visit the route to confirm the app is live.

If anything is unclear, the agent should determine missing details from the repo and proceed with the minimal deployment steps needed to get the PR running.
