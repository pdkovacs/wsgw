FROM ubuntu:22.04

LABEL Name="wsgw"
LABEL Version="1.0.0"

RUN useradd -m --shell /bin/false wsgw

RUN apt-get update && apt-get install -y git curl && apt-get clean
RUN mkdir -p /opt/wsgw
WORKDIR /opt/wsgw
COPY wsgw ./
RUN chmod +x wsgw
RUN ls -la /opt/
EXPOSE 8080

USER wsgw

CMD [ "/opt/wsgw/wsgw" ]
