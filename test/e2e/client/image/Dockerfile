FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends iproute2 netcat-traditional dnsutils curl \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -u 1101 -m --shell /bin/false wsgw

COPY --chown=root:root --chmod=555 ./wsgw-e2e-client /bin/

LABEL service.type="application"
EXPOSE 2223

USER wsgw

CMD [ "/bin/wsgw-e2e-client" ]
