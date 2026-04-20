#!/usr/bin/env python3
"""
scripts/export_onnx.py

Fine-tune microsoft/deberta-v3-small for multi-task network quality
classification and export to ONNX.

Requirements:
    pip install transformers torch onnx onnxruntime sentencepiece

Outputs (written to current directory or --out-dir):
    netmonit-classifier.onnx  — ONNX inference graph  (~190 MB)
    spiece.model              — SentencePiece vocabulary (DeBERTa tokenizer)

Model output layout  (9 logits):
    [0:3]  latency   good / moderate / critical
    [3:6]  loss      none / minor    / severe
    [6:9]  jitter    stable / variable / unstable

Usage:
    python scripts/export_onnx.py
    python scripts/export_onnx.py --out-dir artifacts --epochs 5
"""

import argparse
import os
import random
import shutil
import sys

import numpy as np
import torch
import torch.nn as nn
from torch.utils.data import DataLoader, Dataset
from transformers import DebertaV2Model, DebertaV2Tokenizer

# ── CLI ───────────────────────────────────────────────────────────────────────
parser = argparse.ArgumentParser()
parser.add_argument("--out-dir",   default=".",     help="Output directory")
parser.add_argument("--model-id",  default="microsoft/deberta-v3-small")
parser.add_argument("--seq-len",   type=int, default=128)
parser.add_argument("--batch",     type=int, default=16)
parser.add_argument("--epochs",    type=int, default=3)
parser.add_argument("--lr",        type=float, default=2e-5)
parser.add_argument("--seed",      type=int, default=42)
parser.add_argument("--n-train",   type=int, default=4000)
parser.add_argument("--n-val",     type=int, default=800)
args = parser.parse_args()

random.seed(args.seed)
torch.manual_seed(args.seed)
os.makedirs(args.out_dir, exist_ok=True)

ONNX_PATH  = os.path.join(args.out_dir, "netmonit-classifier.onnx")
SPM_PATH   = os.path.join(args.out_dir, "spiece.model")

# ── Synthetic data ─────────────────────────────────────────────────────────────
def make_samples(n: int) -> list[tuple[str, int, int, int]]:
    """
    Returns a list of (text, lat_cls, loss_cls, jit_cls).

    Class boundaries match the rule-based fallback in deberta.go:
      latency : good  <50ms | moderate 50-299ms | critical >=300ms
      loss    : none  0%    | minor    <2%       | severe   >=2%
      jitter  : stable <20ms| variable 20-49ms   | unstable >=50ms
    """
    samples: list[tuple[str, int, int, int]] = []
    for _ in range(n):
        # ── latency ──────────────────────────────────────────────────────────
        lat_cls = random.randint(0, 2)
        if lat_cls == 0:           # good
            avg = random.uniform(1,  49)
            worst = avg + random.uniform(0, 30)
        elif lat_cls == 1:         # moderate
            avg = random.uniform(50, 299)
            worst = avg + random.uniform(0, 100)
        else:                      # critical
            avg = random.uniform(300, 900)
            worst = avg + random.uniform(0, 400)

        # ── loss ─────────────────────────────────────────────────────────────
        loss_cls = random.randint(0, 2)
        if loss_cls == 0:          # none
            loss_pct = 0.0
        elif loss_cls == 1:        # minor
            loss_pct = random.uniform(0.1, 1.9)
        else:                      # severe
            loss_pct = random.uniform(2.0, 40.0)

        # ── jitter ───────────────────────────────────────────────────────────
        jit_cls = random.randint(0, 2)
        if jit_cls == 0:           # stable
            jitter = random.uniform(0, 19)
        elif jit_cls == 1:         # variable
            jitter = random.uniform(20, 49)
        else:                      # unstable
            jitter = random.uniform(50, 250)

        # vary phrasing slightly so the model generalises beyond exact format
        if random.random() < 0.2:
            text = (f"latency avg {avg:.0f}ms peak {worst:.0f}ms "
                    f"packet-loss {loss_pct:.1f}% jitter {jitter:.0f}ms")
        else:
            text = (f"avg {avg:.1f}ms worst {worst:.1f}ms "
                    f"{loss_pct:.1f}% loss jitter {jitter:.1f}ms")

        samples.append((text, lat_cls, loss_cls, jit_cls))
    return samples


# ── Dataset ───────────────────────────────────────────────────────────────────
class NetQualityDataset(Dataset):
    def __init__(self, samples: list, tokenizer: DebertaV2Tokenizer, seq_len: int):
        self.samples   = samples
        self.tokenizer = tokenizer
        self.seq_len   = seq_len

    def __len__(self) -> int:
        return len(self.samples)

    def __getitem__(self, idx: int) -> dict:
        text, lat, loss, jit = self.samples[idx]
        enc = self.tokenizer(
            text,
            max_length=self.seq_len,
            padding="max_length",
            truncation=True,
            return_tensors="pt",
        )
        return {
            "input_ids":      enc["input_ids"].squeeze(0),
            "attention_mask": enc["attention_mask"].squeeze(0),
            "lat":  torch.tensor(lat,  dtype=torch.long),
            "loss": torch.tensor(loss, dtype=torch.long),
            "jit":  torch.tensor(jit,  dtype=torch.long),
        }


# ── Model ─────────────────────────────────────────────────────────────────────
class NetMonitClassifier(nn.Module):
    """
    DeBERTa-v3-small backbone + three linear heads.

    Forward returns a single concatenated logit tensor [batch, 9] so that
    ONNX export produces exactly one named output ("logits").
    """
    def __init__(self, backbone: DebertaV2Model):
        super().__init__()
        self.backbone   = backbone
        hidden          = backbone.config.hidden_size   # 768 for deberta-v3-small
        self.head_lat   = nn.Linear(hidden, 3)
        self.head_loss  = nn.Linear(hidden, 3)
        self.head_jit   = nn.Linear(hidden, 3)
        self._init_heads()

    def _init_heads(self):
        for head in (self.head_lat, self.head_loss, self.head_jit):
            nn.init.normal_(head.weight, std=0.02)
            nn.init.zeros_(head.bias)

    def forward(
        self,
        input_ids:      torch.Tensor,
        attention_mask: torch.Tensor,
    ) -> torch.Tensor:
        out = self.backbone(
            input_ids=input_ids,
            attention_mask=attention_mask,
        )
        cls     = out.last_hidden_state[:, 0, :]    # [batch, hidden]
        logits  = torch.cat([
            self.head_lat(cls),
            self.head_loss(cls),
            self.head_jit(cls),
        ], dim=-1)                                  # [batch, 9]
        return logits


# ── Training ──────────────────────────────────────────────────────────────────
def train() -> tuple["NetMonitClassifier", DebertaV2Tokenizer]:
    device = "cuda" if torch.cuda.is_available() else "cpu"
    print(f"[train] device={device}  model={args.model_id}")

    tokenizer = DebertaV2Tokenizer.from_pretrained(args.model_id)
    backbone  = DebertaV2Model.from_pretrained(args.model_id)
    model     = NetMonitClassifier(backbone).to(device)

    print(f"[train] generating {args.n_train} train + {args.n_val} val samples")
    train_ds = NetQualityDataset(make_samples(args.n_train), tokenizer, args.seq_len)
    val_ds   = NetQualityDataset(make_samples(args.n_val),   tokenizer, args.seq_len)
    train_dl = DataLoader(train_ds, batch_size=args.batch, shuffle=True,  num_workers=0)
    val_dl   = DataLoader(val_ds,   batch_size=args.batch, shuffle=False, num_workers=0)

    optimizer = torch.optim.AdamW(model.parameters(), lr=args.lr, weight_decay=0.01)
    criterion = nn.CrossEntropyLoss()

    for epoch in range(1, args.epochs + 1):
        # ── train step ───────────────────────────────────────────────────────
        model.train()
        total_loss = 0.0
        for batch in train_dl:
            ids    = batch["input_ids"].to(device)
            mask   = batch["attention_mask"].to(device)
            l_lat  = batch["lat"].to(device)
            l_loss = batch["loss"].to(device)
            l_jit  = batch["jit"].to(device)

            logits = model(ids, mask)
            loss = (
                criterion(logits[:, 0:3], l_lat)  +
                criterion(logits[:, 3:6], l_loss) +
                criterion(logits[:, 6:9], l_jit)
            ) / 3.0

            optimizer.zero_grad()
            loss.backward()
            torch.nn.utils.clip_grad_norm_(model.parameters(), 1.0)
            optimizer.step()
            total_loss += loss.item()

        avg_loss = total_loss / len(train_dl)

        # ── validation step ──────────────────────────────────────────────────
        model.eval()
        correct = [0, 0, 0]
        total   = 0
        with torch.no_grad():
            for batch in val_dl:
                ids  = batch["input_ids"].to(device)
                mask = batch["attention_mask"].to(device)
                logits = model(ids, mask)
                for i, key in enumerate(("lat", "loss", "jit")):
                    pred      = logits[:, i * 3:(i + 1) * 3].argmax(dim=-1)
                    correct[i] += (pred == batch[key].to(device)).sum().item()
                total += ids.size(0)

        print(
            f"  epoch {epoch}/{args.epochs}  "
            f"train_loss={avg_loss:.4f}  "
            f"val_acc  lat={correct[0]/total:.3f}  "
            f"loss={correct[1]/total:.3f}  "
            f"jit={correct[2]/total:.3f}"
        )

    return model, tokenizer


# ── ONNX export ───────────────────────────────────────────────────────────────
def export_onnx(model: "NetMonitClassifier", tokenizer: DebertaV2Tokenizer) -> None:
    print(f"\n[export] exporting to {ONNX_PATH} ...")
    model.eval().cpu()

    dummy_ids  = torch.zeros(1, args.seq_len, dtype=torch.int64)
    dummy_mask = torch.ones (1, args.seq_len, dtype=torch.int64)

    torch.onnx.export(
        model,
        args=(dummy_ids, dummy_mask),
        f=ONNX_PATH,
        input_names=["input_ids", "attention_mask"],
        output_names=["logits"],
        dynamic_axes={
            "input_ids":      {0: "batch"},
            "attention_mask": {0: "batch"},
            "logits":         {0: "batch"},
        },
        opset_version=14,
        do_constant_folding=True,
        export_params=True,
    )

    size_mb = os.path.getsize(ONNX_PATH) / 1e6
    print(f"[export] ✓  {ONNX_PATH}  ({size_mb:.1f} MB)")

    # ── copy spiece.model from tokenizer cache ────────────────────────────────
    spm_src = getattr(tokenizer, "vocab_file", None)
    if spm_src and os.path.isfile(spm_src):
        shutil.copy(spm_src, SPM_PATH)
        print(f"[export] ✓  {SPM_PATH}  ({os.path.getsize(SPM_PATH)/1e3:.0f} KB)")
    else:
        # Fall back: search the HuggingFace model cache directory
        import huggingface_hub
        try:
            cache_dir = huggingface_hub.snapshot_download(
                args.model_id, local_files_only=True
            )
            src = os.path.join(cache_dir, "spiece.model")
            if os.path.isfile(src):
                shutil.copy(src, SPM_PATH)
                print(f"[export] ✓  {SPM_PATH}  (from hub cache)")
            else:
                print("[export] ⚠  spiece.model not found in cache — run with internet access", file=sys.stderr)
        except Exception as e:
            print(f"[export] ⚠  could not locate spiece.model: {e}", file=sys.stderr)


# ── Sanity check ──────────────────────────────────────────────────────────────
def verify_onnx(tokenizer: DebertaV2Tokenizer) -> None:
    import onnxruntime as ort

    print("\n[verify] running ONNX Runtime inference ...")
    sess_opts = ort.SessionOptions()
    sess_opts.intra_op_num_threads = 1
    sess = ort.InferenceSession(ONNX_PATH, sess_options=sess_opts)

    cases = [
        ("avg 20.0ms worst 40.0ms 0.0% loss jitter 5.0ms",  "good",     "none",  "stable"),
        ("avg 100.0ms worst 180.0ms 0.5% loss jitter 30.0ms", "moderate", "minor", "variable"),
        ("avg 500.0ms worst 900.0ms 5.0% loss jitter 80.0ms", "critical", "severe","unstable"),
    ]
    lat_labels  = ["good",   "moderate", "critical"]
    loss_labels = ["none",   "minor",    "severe"]
    jit_labels  = ["stable", "variable", "unstable"]

    all_ok = True
    for text, exp_lat, exp_loss, exp_jit in cases:
        enc = tokenizer(
            text,
            max_length=args.seq_len,
            padding="max_length",
            truncation=True,
            return_tensors="np",
        )
        out    = sess.run(["logits"], {
            "input_ids":      enc["input_ids"].astype(np.int64),
            "attention_mask": enc["attention_mask"].astype(np.int64),
        })
        logits = out[0][0]
        pred_lat  = lat_labels [np.argmax(logits[0:3])]
        pred_loss = loss_labels[np.argmax(logits[3:6])]
        pred_jit  = jit_labels [np.argmax(logits[6:9])]
        ok = (pred_lat == exp_lat and pred_loss == exp_loss and pred_jit == exp_jit)
        icon = "✓" if ok else "✗"
        print(f"  {icon}  lat={pred_lat:<8} loss={pred_loss:<6} jit={pred_jit:<8}  | {text[:50]}")
        all_ok = all_ok and ok

    if all_ok:
        print("[verify] ✓ all cases passed")
    else:
        print("[verify] ⚠ some cases mismatch — consider more epochs or data", file=sys.stderr)


# ── Entry point ───────────────────────────────────────────────────────────────
if __name__ == "__main__":
    model, tokenizer = train()
    export_onnx(model, tokenizer)
    verify_onnx(tokenizer)

    print("\nNext steps:")
    print(f"  gh release upload <tag> {ONNX_PATH} {SPM_PATH}")
