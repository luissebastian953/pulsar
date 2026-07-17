# ComfyUI runtime for the GPU deploy target: linux + NVIDIA driver injected by
# nvidia-container-toolkit (compose `gpu` profile). Dev machines without NVIDIA
# run natively instead — ./run-native.sh reads the same pin below.
FROM python:3.13-slim

ARG COMFYUI_REF=v0.28.0
# cu130 wheels bundle the CUDA runtime, so a plain python base works — the host
# supplies only the driver. No dependency on unofficial ComfyUI images.
ARG TORCH_INDEX=https://download.pytorch.org/whl/cu130

RUN apt-get update \
 && apt-get install -y --no-install-recommends git \
 && rm -rf /var/lib/apt/lists/*

RUN git clone --depth 1 --branch ${COMFYUI_REF} https://github.com/Comfy-Org/ComfyUI /app
WORKDIR /app

RUN pip install --no-cache-dir torch torchvision torchaudio --index-url ${TORCH_INDEX} \
 && pip install --no-cache-dir -r requirements.txt

# Custom nodes: none yet. When a workflow gains a dependency on one, pin it by
# commit here in the same change (.claude/skills/comfy-workflow/):
#   RUN git clone https://github.com/cubiq/ComfyUI_IPAdapter_plus custom_nodes/ComfyUI_IPAdapter_plus \
#    && git -C custom_nodes/ComfyUI_IPAdapter_plus checkout <sha> \
#    && pip install --no-cache-dir -r custom_nodes/ComfyUI_IPAdapter_plus/requirements.txt

COPY extra_model_paths.yaml /app/extra_model_paths.yaml

# Weights are never baked in — 6.9 GB per checkpoint, and the licences aren't
# ours to redistribute. fetch-models.sh populates the host dir mounted here.
VOLUME /models
RUN mkdir -p /data/output /data/input
VOLUME /data

EXPOSE 8188

HEALTHCHECK --interval=15s --timeout=5s --start-period=120s --retries=5 \
  CMD python -c "import urllib.request as u; u.urlopen('http://127.0.0.1:8188/system_stats', timeout=4)"

CMD ["python", "main.py", "--listen", "0.0.0.0", "--port", "8188", \
     "--extra-model-paths-config", "extra_model_paths.yaml", \
     "--output-directory", "/data/output", "--input-directory", "/data/input"]
