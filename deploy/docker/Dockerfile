FROM debian:12-slim

LABEL Name="wsgw"
LABEL Version="1.0.0"

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl iproute2 ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m --shell /bin/false wsgw

WORKDIR /opt/wsgw
COPY --chown=root:root --chmod=555 wsgw ./
EXPOSE 8080

USER wsgw

CMD [ "/opt/wsgw/wsgw" ]
