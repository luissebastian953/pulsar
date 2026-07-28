#!/usr/bin/env bash
# Character LoRA training on the NoobAI v-pred base (MPS-tuned).
#
# v-pred REQUIRES --v_parameterization + --zero_terminal_snr — without them
# the LoRA learns against the wrong objective and produces garbage, the same
# failure mode as sampling v-pred without ModelSamplingDiscrete
# (docs/tuning.md). UNet-only + cached latents/text-encoder keeps the
# footprint inside ~12 GB unified memory; quit heavy apps first.
#
# Usage: train/train-lora.sh [output-name]   (default: yixuan-noobai-vpred)
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
NAME="${1:-yixuan-noobai-vpred}"
CKPT="$DIR/../comfy/models/checkpoints/noobai-xl-vpred.safetensors"
[ -f "$CKPT" ] || { echo "checkpoint missing: $CKPT" >&2; exit 1; }
mkdir -p "$DIR/output"

# run from train/ so dataset.toml's relative image_dir resolves; the
# editable install makes kohya's library importable from anywhere
cd "$DIR"
exec "$DIR/.venv/bin/python" "$DIR/sd-scripts/sdxl_train_network.py" \
  --pretrained_model_name_or_path="$CKPT" \
  --v_parameterization --zero_terminal_snr \
  --dataset_config="$DIR/dataset.toml" \
  --output_dir="$DIR/output" --output_name="$NAME" \
  --save_model_as=safetensors \
  --network_module=networks.lora --network_dim=16 --network_alpha=8 \
  --network_train_unet_only \
  --cache_latents --cache_latents_to_disk --cache_text_encoder_outputs \
  --gradient_checkpointing \
  --max_train_steps=1600 --save_every_n_steps=400 \
  --learning_rate=1e-4 --lr_scheduler=cosine --lr_warmup_steps=100 \
  --optimizer_type=AdamW \
  --mixed_precision=no --save_precision=fp16 \
  --seed=7777 \
  --logging_dir="$DIR/logs"
