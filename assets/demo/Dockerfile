FROM ghcr.io/charmbracelet/vhs:v0.11.0

RUN apt-get update \
    && apt-get install --yes --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*
