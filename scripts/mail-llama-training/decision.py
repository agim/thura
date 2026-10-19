"""One example, one three-way class loss; no JSON or end-token supervision.

Only the prompt-end hidden state is projected. This avoids allocating a
sequence-length by 128k-vocabulary tensor and keeps the loss about classification.
"""
import functools
import json
from pathlib import Path

PROMPT = Path(__file__).with_name('decisionbench') / 'prompt.json'
CONTRACT = json.loads(PROMPT.read_text())
CODES = CONTRACT['codes']
LABELS = ['legitimate', 'spam', 'phishing']
TOKEN_IDS = [32, 33, 34]


def validate_example(messages, tokenizer, maximum=4096):
    if len(messages) != 3 or [m['role'] for m in messages] != ['system', 'user', 'assistant']:
        raise ValueError('invalid classification conversation')
    if messages[0]['content'] != CONTRACT['system'] or messages[-1]['content'] not in CODES.values():
        raise ValueError('classification contract or label mismatch')
    prefix = tokenizer.apply_chat_template(messages[:2], add_generation_prompt=True, return_dict=False)
    tokens = tokenizer.apply_chat_template(messages, return_dict=False)
    expected = TOKEN_IDS[[CODES[name] for name in LABELS].index(messages[-1]['content'])]
    if tokens[:len(prefix)] != prefix or tokens[len(prefix):] != [expected, tokenizer.eos_token_id]:
        raise ValueError('class must be one token followed by end-of-turn; no hidden formatting target')
    if len(tokens) > maximum:
        raise ValueError('overlong classification input; never truncate')
    return tokens, len(prefix)


def class_scores(model, batch, offsets):
    import mlx.core as mx
    positions = mx.arange(batch.shape[1])[None, :]
    # Strip the entire gold completion from forward input, including padded rows.
    inputs = mx.where(positions < offsets[:, None], batch, 0)
    hidden = model.model(inputs)
    selected = hidden[mx.arange(batch.shape[0]), offsets - 1]
    logits = model.model.embed_tokens.as_linear(selected) if model.args.tie_word_embeddings else model.lm_head(selected)
    return mx.take(logits, mx.array(TOKEN_IDS), axis=-1).astype(mx.float32)


def classification_loss(model, batch, lengths):
    import mlx.core as mx
    import mlx.nn as nn
    offsets = lengths[:, 0]
    targets = batch[mx.arange(batch.shape[0]), offsets]
    indices = mx.argmax(targets[:, None] == mx.array(TOKEN_IDS)[None, :], axis=-1)
    scores = class_scores(model, batch, offsets)
    return nn.losses.cross_entropy(scores, indices, reduction='mean'), mx.array(batch.shape[0])


def main():
    import mlx.core as mx
    import mlx_lm.lora as lora
    # Use upstream LoRA/optimizer/checkpointing machinery with an explicit loss.
    # No installed package or upstream default is modified.
    lora.train = functools.partial(lora.train, loss=classification_loss)
    lora.main()
    print('THURA_MLX_MEMORY=' + json.dumps({'peak_metal_bytes': mx.get_peak_memory()}))


if __name__ == '__main__':
    main()
