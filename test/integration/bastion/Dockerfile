# Minimal SSH bastion for dbcli tunnel integration tests. TCP forwarding is
# explicitly enabled (the whole point) and a password user is provisioned.
# Uses the aliyun alpine mirror (publicly reachable; fast from CN where the
# default dl-cdn.alpinelinux.org is throttled).
FROM alpine:3.20
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories && \
    apk add --no-cache openssh-server && \
    ssh-keygen -A && \
    adduser -D deploy && echo 'deploy:tunnelpw' | chpasswd && \
    sed -i 's/#\?AllowTcpForwarding.*/AllowTcpForwarding yes/' /etc/ssh/sshd_config && \
    sed -i 's/#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/#\?GatewayPorts.*/GatewayPorts yes/' /etc/ssh/sshd_config
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
