# Based on: https://gitlab.winehq.org/wine/wine/-/wikis/Debian-Ubuntu
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update -y && \
    apt-get install -y --install-recommends wget gpg && \
    mkdir -pm755 /etc/apt/keyrings && \
    dpkg --add-architecture i386 && \
    wget -O - https://dl.winehq.org/wine-builds/winehq.key | gpg --dearmor -o /etc/apt/keyrings/winehq-archive.key - && \
    wget -NP /etc/apt/sources.list.d/ https://dl.winehq.org/wine-builds/ubuntu/dists/noble/winehq-noble.sources && \
    apt-get update -y && \
    apt-get install -y --install-recommends winehq-stable winetricks xvfb && \
    rm -rf /var/lib/apt/lists/* && \
    apt-get clean

ENV WINEPREFIX=/opt/wine/prefix

RUN mkdir -p "${WINEPREFIX}"
RUN wineboot -i && wineserver -w
