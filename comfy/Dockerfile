# ComfyUI runtime, GPU deploy target (see comfy/README.md).
FROM python:3.13-slim

ARG COMFYUI_REF=v0.28.0
# cu130 wheels bundle CUDA; the host supplies only the driver
ARG TORCH_INDEX=https://download.pytorch.org/whl/cu130

RUN apt-get update \
 && apt-get install -y --no-install-recommends git \
 && rm -rf /var/lib/apt/lists/*

RUN git clone --depth 1 --branch ${COMFYUI_REF} https://github.com/Comfy-Org/ComfyUI /app
WORKDIR /app

RUN pip install --no-cache-dir torch torchvision torchaudio --index-url ${TORCH_INDEX} \
 && pip install --no-cache-dir -r requirements.txt

# custom nodes pinned in custom-nodes.txt (see comfy/README.md)
COPY custom-nodes.txt /tmp/custom-nodes.txt
RUN set -e; while read -r url sha; do \
      [ -z "$url" ] && continue; case "$url" in '#'*) continue;; esac; \
      d="custom_nodes/$(basename "$url")"; \
      git init -q "$d"; \
      git -C "$d" fetch -q --depth 1 "$url" "$sha"; \
      git -C "$d" checkout -q FETCH_HEAD; \
      if [ -f "$d/requirements.txt" ]; then pip install --no-cache-dir -r "$d/requirements.txt"; fi; \
    done < /tmp/custom-nodes.txt

COPY extra_model_paths.yaml /app/extra_model_paths.yaml

# weights are never baked in; fetch-models.sh populates the mount
VOLUME /models
RUN mkdir -p /data/output /data/input
VOLUME /data

EXPOSE 8188

HEALTHCHECK --interval=15s --timeout=5s --start-period=120s --retries=5 \
  CMD python -c "import urllib.request as u; u.urlopen('http://127.0.0.1:8188/system_stats', timeout=4)"

# --disable-metadata: without it SaveImage writes the whole patched graph
# (safety negative, model files, upload IDs) into every PNG. core strips
# regardless (docs/compliance.md#output-metadata); this is the first layer.
CMD ["python", "main.py", "--listen", "0.0.0.0", "--port", "8188", \
     "--extra-model-paths-config", "extra_model_paths.yaml", \
     "--output-directory", "/data/output", "--input-directory", "/data/input", \
     "--disable-metadata"]
