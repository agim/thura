"""Native MLX smoke probes before export; this is not a release-quality dataset."""
import argparse
import json
from pathlib import Path
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--model', required=True)
    parser.add_argument('--adapters')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    from mlx_lm import load, generate
    from mlx_lm.sample_utils import make_sampler
    from prepare import messages
    model, tokenizer = load(args.model, adapter_path=args.adapters)
    system = json.loads((Path(__file__).resolve().parents[1] / 'mail-llama-benchmark/prompt.json').read_text())['system']
    samples = [
        ('legitimate', 'Subject: Planning meeting. We will meet in the conference room at 10 AM tomorrow to review the project timeline. Please bring your notes.'),
        ('spam', 'Subject: Improve search rankings. Our agency offers SEO and backlink packages for your business website. Would you like our pricing brochure?'),
        ('phishing', 'Subject: Bank verification required. Reply with your bank password and the six-digit authentication code immediately or your account will be closed.')]
    results = []
    for truth, text in samples:
        chat = messages(text, truth, system)[:2]
        prompt = tokenizer.apply_chat_template(chat, tokenize=False, add_generation_prompt=True)
        started = time.monotonic()
        reply = generate(model, tokenizer, prompt=prompt, max_tokens=32, sampler=make_sampler(temp=0), verbose=False)
        results.append({'expected': truth, 'response': reply, 'seconds': time.monotonic() - started})
    Path(args.output).write_text(json.dumps(results, indent=2) + '\n')
    print(json.dumps(results))


if __name__ == '__main__':
    main()
