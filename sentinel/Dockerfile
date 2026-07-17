# Compliance classifier sidecar (docs/compliance.md).
FROM python:3.13-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends curl \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY serve.py fetch-model.sh ./

# model fetched into the volume at first start; sha-verified
VOLUME /models
ENV SENTINEL_MODEL_DIR=/models
EXPOSE 8189

HEALTHCHECK --interval=15s --timeout=5s --start-period=300s --retries=5 \
  CMD python -c "import urllib.request as u; u.urlopen('http://127.0.0.1:8189/healthz', timeout=4)"

CMD ["sh", "-c", "./fetch-model.sh && exec python serve.py"]
