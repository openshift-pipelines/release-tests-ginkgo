FROM quay.io/fedora/fedora:44

RUN dnf update -y &&\
    dnf install -y --setopt=tsflags=nodocs azure-cli git go jq make openssl python-unversioned-command python3 python3-antlr4-runtime python3-pip skopeo unzip vim wget yq && \
    dnf clean all -y && rm -fR /var/cache/dnf

RUN pip install pyyaml reportportal-client

RUN wget https://certs.corp.redhat.com/certs/Current-IT-Root-CAs.pem \
    -O /etc/pki/ca-trust/source/anchors/Current-IT-Root-CAs.pem && \
    update-ca-trust extract

ENV OC_VERSION=4.19
RUN wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/fast-${OC_VERSION}/openshift-client-linux.tar.gz \
    -O /tmp/openshift-client.tar.gz &&\
    tar xzf /tmp/openshift-client.tar.gz -C /usr/bin oc &&\
    rm /tmp/openshift-client.tar.gz

RUN wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/fast-${OC_VERSION}/oc-mirror.tar.gz \
    -O /tmp/oc-mirror.tar.gz &&\
    tar xzf /tmp/oc-mirror.tar.gz -C /usr/bin oc-mirror &&\
    chmod u+x /usr/bin/oc-mirror &&\
    rm /tmp/oc-mirror.tar.gz

RUN wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/fast-${OC_VERSION}/opm-linux.tar.gz \
    -O /tmp/opm.tar.gz &&\
    tar xzf /tmp/opm.tar.gz -C /usr/bin opm-rhel8 &&\
    mv /usr/bin/opm-rhel8 /usr/bin/opm &&\
    chmod u+x /usr/bin/opm &&\
    rm /tmp/opm.tar.gz

RUN wget https://mirror.openshift.com/pub/openshift-v4/clients/rosa/latest/rosa-linux.tar.gz \
    -O /tmp/rosa.tar.gz &&\
    tar xzf /tmp/rosa.tar.gz -C /usr/bin --no-same-owner rosa &&\
    rm /tmp/rosa.tar.gz

ENV TKN_VERSION=1.20.0
RUN wget https://mirror.openshift.com/pub/openshift-v4/clients/pipelines/${TKN_VERSION}/tkn-linux-amd64.tar.gz \
   -O /tmp/tkn.tar.gz &&\
   tar xzf /tmp/tkn.tar.gz -C /usr/bin --no-same-owner tkn tkn-pac opc &&\
   rm /tmp/tkn.tar.gz

RUN wget https://dl.min.io/client/mc/release/linux-amd64/mc -O /usr/bin/mc &&\
    chmod u+x /usr/bin/mc

# Ginkgo CLI (replaces Gauge)
ENV GINKGO_VERSION=v2.28.1
RUN go install github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION} &&\
    ln -s /usr/bin/oc /usr/bin/kubectl &&\
    go env -w GOPROXY="https://proxy.golang.org,direct"

ENV PATH=$GOPATH/bin:$PATH

RUN wget https://github.com/sigstore/cosign/releases/download/v3.0.3/cosign-linux-amd64 -O /usr/bin/cosign && \
    chmod a+x /usr/bin/cosign

RUN wget https://github.com/sigstore/rekor/releases/download/v1.4.3/rekor-cli-linux-amd64 -O /usr/bin/rekor-cli && \
    chmod u+x /usr/bin/rekor-cli

ENV GOLANGCI_LINT_VERSION=2.7.2
RUN wget -O /tmp/golangci-lint.tar.gz https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz \
    && tar --strip-components=1 -C /usr/bin -xzf /tmp/golangci-lint.tar.gz golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64/golangci-lint \
    && rm -f /tmp/golangci-lint.tar.gz

ENV GOMODCACHE=/opt/go/pkg/mod
COPY go.mod go.sum /tmp/release-tests-ginkgo/
RUN (cd /tmp/release-tests-ginkgo && \
    go mod download) && \
    rm -rf /tmp/release-tests-ginkgo && \
    chgrp -R 0 /opt/go && \
    chmod -R g=u /opt/go

COPY . /tmp/release-tests-ginkgo
WORKDIR /tmp/release-tests-ginkgo

RUN chgrp -R 0 /tmp && chmod -R g=u /tmp

CMD ["/bin/bash"]
