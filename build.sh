docker buildx build -t linkerd2-proxy-init:local .
docker tag docker.io/library/linkerd2-proxy-init:local australia-southeast1-docker.pkg.dev/gb-plat-poc-spaces-pt-poc-v3/plat-docker-repo/linkerd/proxy-init:v1.3.6
docker push australia-southeast1-docker.pkg.dev/gb-plat-poc-spaces-pt-poc-v3/plat-docker-repo/linkerd/proxy-init:v1.3.6
