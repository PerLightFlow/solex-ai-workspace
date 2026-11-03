# The daemon image is selected by administrators; no host paths or credentials
# are mounted into a conversation container.
ARG PYTHON_IMAGE=python:3.12-slim
FROM ${PYTHON_IMAGE}
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash coreutils findutils git ca-certificates curl jq unzip file \
    && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir openpyxl==3.1.5 Pillow==11.3.0
RUN mkdir -p /workspace/input /workspace/output /var/lib/weknora
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1
WORKDIR /workspace
CMD ["sleep", "infinity"]
