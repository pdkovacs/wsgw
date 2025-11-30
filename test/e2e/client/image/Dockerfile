FROM ubuntu

RUN apt-get update && apt-get install iproute2 netcat-traditional dnsutils curl -y
COPY ./wsgw-e2e-client /bin
LABEL service.type="application"
EXPOSE 2223
CMD [ "/bin/wsgw-e2e-client" ]
