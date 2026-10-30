FROM golang:1.26.10-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb

RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 rpm cpio libssl-dev libcurl4-openssl-dev libexpat1-dev zlib1g-dev \
    && rm -rf /var/lib/apt/lists/*

# The base image's Git predates the project's Git 2.51 baseline.
# Source checksum: https://www.kernel.org/pub/software/scm/git/sha256sums.asc
RUN curl -fsSLo /tmp/git.tar.gz https://www.kernel.org/pub/software/scm/git/git-2.51.0.tar.gz \
    && echo '3d531799d2cf2cac8e294ec6e3229e07bfca60dc6c783fe69e7712738bef7283  /tmp/git.tar.gz' | sha256sum -c - \
    && tar -xzf /tmp/git.tar.gz -C /tmp \
    && make -C /tmp/git-2.51.0 -j4 prefix=/opt/git NO_GETTEXT=YesPlease NO_TCLTK=YesPlease install \
    && rm -rf /tmp/git-2.51.0 /tmp/git.tar.gz

ENV PATH="/opt/git/bin:${PATH}"
COPY check-linux.sh /opt/check-linux.sh
RUN sed -i 's/\r$//' /opt/check-linux.sh
ENTRYPOINT ["bash", "/opt/check-linux.sh"]
