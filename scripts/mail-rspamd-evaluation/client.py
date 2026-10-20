"""One-pass raw-MIME evaluation; no labels enter requests and no model learning occurs."""
import collections
import hashlib
import http.client
import json
import math
from pathlib import Path
import sys
import time

PHISHING = {'PHISHING', 'PHISHED_URL', 'PHISHED_GENERIC_SERVICE', 'PHISHED_OPENPHISH', 'PHISHED_PHISHTANK'}


def classify(reply):
    if reply.get('is_skipped') or reply.get('error'):
        raise ValueError('message was not completely scanned')
    score = float(reply['score'])
    if not math.isfinite(score):
        raise ValueError('nonfinite score')
    symbols = reply.get('symbols', {})
    phishing = sorted(PHISHING & set(symbols))
    if phishing:
        return 'phishing', False, phishing
    if score >= 6:
        return 'spam', False, []
    # A rule score is not a calibrated confidence probability.
    return 'legitimate', score >= 1, []


def impossible(results, totals):
    """Conclusive rejection bounds on the fixed full cohort, never denominator shrinkage."""
    fresh = [r for r in results if r['source'] not in ['instruction', 'tail']]
    fp = sum(r['label'] == 'legitimate' and r['prediction'] != 'legitimate' for r in fresh)
    misses = sum(r['label'] == 'phishing' and r['prediction'] != 'phishing' for r in fresh)
    spam_misses = sum(r['label'] == 'spam' and r['prediction'] != 'spam' for r in fresh)
    spam_phish = sum(r['label'] == 'spam' and r['prediction'] == 'phishing' for r in fresh)
    reasons = []
    if fp > math.floor(totals['legitimate'] * .02 + 1e-9): reasons.append('legitimate false positives irrecoverably exceed 2%')
    if misses > totals['phishing'] - math.ceil(totals['phishing'] * .95): reasons.append('phishing detection cannot reach 95%')
    if spam_misses > totals['spam'] - math.ceil(totals['spam'] * .90): reasons.append('ordinary spam detection cannot reach 90%')
    if spam_phish > math.floor(totals['spam'] * .05 + 1e-9): reasons.append('spam-as-phishing irrecoverably exceeds 5%')
    if any(r.get('error') for r in results): reasons.append('nonzero scan failures')
    return reasons


def main():
    contract = json.load(sys.stdin)
    rows = contract['cases']
    results = []
    start_run = time.monotonic()
    for row in rows:
        data = (Path('/corpus') / (row['id'] + '.eml')).read_bytes()
        if len(data) > 2 * 1024**2 or hashlib.sha256(data).hexdigest() != row['message_sha256']:
            raise ValueError('frozen raw MIME changed or exceeds bound')
        result = {k: row[k] for k in ['id', 'label', 'source']}
        started = time.monotonic()
        connection = http.client.HTTPConnection('127.0.0.1', 11333, timeout=8)
        try:
            # No fabricated source IP, envelope authentication or label metadata.
            connection.request('POST', '/checkv2', body=data, headers={'Content-Type': 'message/rfc822'})
            response = connection.getresponse()
            payload = response.read(512*1024 + 1)
            if response.status != 200 or len(payload) > 512*1024:
                raise ValueError('failed or unbounded scan response')
            reply = json.loads(payload)
            result['prediction'], result['uncertain_only_review'], result['phishing_symbols'] = classify(reply)
            result['score'] = reply['score']
            result['action'] = reply['action']
            # Scores/names only; options may contain private addresses/URLs.
            result['symbols'] = {k: v.get('score') for k, v in reply.get('symbols', {}).items()}
            result['server_scan_seconds'] = reply.get('time_real')
        except Exception as error:
            result.update(prediction='error', error=type(error).__name__)
        finally:
            connection.close()
        result['millis'] = (time.monotonic()-started)*1000
        results.append(result)
        print('completed', len(results), '/', len(rows), file=sys.stderr, flush=True)
        reasons = impossible(results, contract['planned_counts'])
        if len(results) >= 30 and reasons:
            break
        if time.monotonic()-start_run > 600:
            reasons = ['total evaluation budget exceeded']
            break
    else:
        reasons = impossible(results, contract['planned_counts'])
    groups = {name: Path('/sys/fs/cgroup',name).read_text().strip() for name in
              ['memory.max','memory.peak','memory.swap.max','memory.events','cpu.max','cpu.stat']}
    print(json.dumps({'status': 'rejected_early' if reasons else 'complete_pending_assessment',
                      'reasons': reasons, 'results': results, 'cgroup': groups,
                      'planned_counts': contract['planned_counts'], 'complete': len(results)==len(rows),
                      'elapsed_seconds': time.monotonic()-start_run, 'production_enabled': False}))


if __name__ == '__main__':
    main()
