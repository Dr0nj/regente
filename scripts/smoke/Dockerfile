# Imagem-alvo do smoke test de instalação: um Linux com systemd DE VERDADE, que é
# o que o install-linux.sh precisa (systemctl enable --now, Restart=always,
# journalctl). Construída aqui em vez de usar uma imagem de systemd de terceiros —
# a CI não deve depender de imagem que não controlamos.
#
# curl/git/python3 são do HARNESS, não do produto: o server usa go-git e não
# precisa do binário `git`; curl e python3 só servem pra checar health e ler JSON.
# nginx idem — o produto não depende dele, mas o deploy/vps/README.md manda pôr um
# reverse proxy na frente, e essa fronteira era justamente a que o smoke NÃO
# cobria quando a primeira instalação real quebrou nela.
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq \
 && apt-get install -y -qq --no-install-recommends \
      systemd systemd-sysv ca-certificates curl git python3 nginx \
 && rm -rf /var/lib/apt/lists/* \
 # Unidades que não fazem sentido em container e só sujam o boot.
 && rm -f /lib/systemd/system/multi-user.target.wants/* \
          /etc/systemd/system/*.wants/* \
          /lib/systemd/system/local-fs.target.wants/* \
          /lib/systemd/system/sockets.target.wants/*udev* \
          /lib/systemd/system/basic.target.wants/*

STOPSIGNAL SIGRTMIN+3
CMD ["/lib/systemd/systemd"]
