#!/usr/bin/env bash
# Self-contained cloud bootstrap: run on a rented CUDA box (RunPod/Vast,
# any 24GB card). Installs kohya, fetches the NoobAI v-pred base, trains
# the character LoRA from the bundled dataset, leaves the .safetensors in
# ./output. Same v-pred flags as the local script — they are the part that
# must never be dropped (docs/tuning.md).
#
# Usage (from the unpacked bundle dir): ./train-lora-cloud.sh [output-name]
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
NAME="${1:-yixuan-noobai-vpred}"
CKPT="$DIR/noobai-xl-vpred.safetensors"

if [ ! -d "$DIR/sd-scripts" ]; then
  git clone --depth 1 https://github.com/kohya-ss/sd-scripts.git "$DIR/sd-scripts"
fi
if [ ! -x "$DIR/.venv/bin/python" ]; then
  python3 -m venv "$DIR/.venv"
  "$DIR/.venv/bin/pip" install --quiet --upgrade pip
  "$DIR/.venv/bin/pip" install --quiet torch torchvision --index-url https://download.pytorch.org/whl/cu121
  (cd "$DIR/sd-scripts" && "$DIR/.venv/bin/pip" install --quiet -r requirements.txt)
  "$DIR/.venv/bin/pip" install --quiet bitsandbytes
fi

if [ ! -f "$CKPT" ]; then
  curl -fL --retry 3 -C - -o "$CKPT.partial" \
    "https://huggingface.co/Laxhar/noobai-XL-Vpred-1.0/resolve/main/NoobAI-XL-Vpred-v1.0.safetensors"
  # pinned in comfy/fetch-models.sh; verify here too
  echo "ea349eeae87ca8d25ba902c93810f7ca83e5c82f920edf12f273af004ae02819  $CKPT.partial" | sha256sum -c -
  mv "$CKPT.partial" "$CKPT"
fi

mkdir -p "$DIR/output"
cd "$DIR"
exec "$DIR/.venv/bin/python" "$DIR/sd-scripts/sdxl_train_network.py" \
  --pretrained_model_name_or_path="$CKPT" \
  --v_parameterization --zero_terminal_snr \
  --dataset_config="${DATASET_CONFIG:-$DIR/dataset.toml}" \
  --output_dir="$DIR/output" --output_name="$NAME" \
  --save_model_as=safetensors \
  --network_module=networks.lora --network_dim="${NETWORK_DIM:-16}" --network_alpha="${NETWORK_ALPHA:-8}" \
  --network_train_unet_only \
  --cache_latents --cache_latents_to_disk --cache_text_encoder_outputs \
  --gradient_checkpointing --sdpa \
  --max_train_steps="${MAX_STEPS:-1600}" --save_every_n_steps=400 \
  --learning_rate=1e-4 --lr_scheduler=cosine --lr_warmup_steps=100 \
  --optimizer_type=AdamW8bit \
  --mixed_precision=bf16 --save_precision=fp16 \
  --seed=7777 \
  --logging_dir="$DIR/logs"
