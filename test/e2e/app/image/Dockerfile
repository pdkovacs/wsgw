FROM ubuntu

RUN apt-get update && apt-get install iproute2 netcat-traditional dnsutils curl -y

RUN useradd -u 1101 -m --shell /bin/false wsgw

COPY --chown=root:root --chmod=555 ./wsgw-e2e-app /bin
LABEL service.type="application"
EXPOSE 2223

USER wsgw

CMD [ "/bin/wsgw-e2e-app" ]
