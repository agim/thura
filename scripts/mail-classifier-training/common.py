"""Versioned three-class mail evidence, labels and evaluation; never execute email content."""
import hashlib
from email.errors import MessageError
from html.parser import HTMLParser
import html
import re

LABELS = ['legitimate', 'spam', 'phishing']
VERSION = 'thura-mail-three-class-v1'
MAX_BYTES = 12000


def digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


class VisibleHTML(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.parts, self.links, self.hidden = [], [], 0

    def handle_starttag(self, tag, attrs):
        if tag in ['script', 'style']:
            self.hidden += 1
        if tag == 'a' and not self.hidden:
            for key, value in attrs:
                if key == 'href' and value and len(value) <= 2048:
                    self.links.append(value)
        if tag in ['p', 'div', 'br', 'li']:
            self.parts.append(' ')

    def handle_endtag(self, tag):
        if tag in ['script', 'style'] and self.hidden:
            self.hidden -= 1

    def handle_data(self, value):
        if not self.hidden:
            self.parts.append(value)


def deidentify(text):
    text = html.unescape(text)
    # Preserve domains/link destinations; remove local parts and full IP literals.
    text = re.sub(r'[\w.+%-]+@([\w.-]+\.[A-Za-z]{2,})', r'recipient@\1', text)
    text = re.sub(r'\b(?:\d{1,3}\.){3}\d{1,3}\b', '<ip-address>', text)
    return re.sub(r'\s+', ' ', text).strip()


def email_text(message):
    plain, markup, files = [], [], []
    try:
        serialized_size = len(message.as_bytes())
    except MessageError as error:
        raise ValueError('malformed message headers; manual review required') from error
    if serialized_size > 2 * 1024 * 1024:
        raise ValueError('message exceeds preprocessing limit')
    for part in message.walk():
        if part.get_content_disposition() == 'attachment':
            files.append(str(part.get_filename() or '')[:200])
            continue
        kind = part.get_content_type()
        if part.is_multipart() or kind not in ['text/plain', 'text/html']:
            continue
        payload = part.get_payload(decode=True)
        if payload is None:
            continue
        if len(payload) > 100000:
            raise ValueError('text part exceeds preprocessing limit')
        try:
            text = payload.decode(part.get_content_charset() or 'utf-8', errors='replace')
        except LookupError:
            text = payload.decode('utf-8', errors='replace')
        (plain if kind == 'text/plain' else markup).append(text)
    # Some public mailboxes incorrectly mark HTML as text/plain. Never train on CSS/markup artifacts.
    usable_plain = []
    for text in plain:
        if len(re.findall(r'<(?:html|table|div|br|a|p)\b', text, re.I)) >= 3:
            markup.append(text)
        else:
            usable_plain.append(text)
    parser = VisibleHTML()
    parser.feed('\n'.join(markup))
    body = '\n'.join(usable_plain) if usable_plain else ' '.join(parser.parts)
    if len(re.findall(r'\w+', body)) < 8:
        raise ValueError('image/attachment-only or insufficient readable evidence')
    value = '\n'.join([f"Subject: {message.get('Subject', '')}", f"From: {message.get('From', '')}",
                        f"Reply-To: {message.get('Reply-To', '')}", body,
                        'Link destinations: ' + ' '.join(parser.links), 'Attachments: ' + ' '.join(files)])
    text = deidentify(value)
    if len(text.encode()) > MAX_BYTES:
        raise ValueError('evidence exceeds serving limit; no truncation')
    return text


def canonical(text):
    value = text.casefold()
    value = re.sub(r'https?://\S+|[\w.+%-]+@[\w.-]+', ' ADDRESS ', value)
    value = re.sub(r'\d+', ' NUMBER ', value)
    return ' '.join(re.findall(r'[a-z]+', value))


def shingles(text):
    words = canonical(text).split()
    return set(' '.join(words[i:i + 5]) for i in range(max(1, len(words) - 4)))


def windows(ids):
    return [ids[i:i + 510] for i in range(0, max(1, len(ids)), 446)]


def classify(scores, phishing_threshold, spam_threshold):
    if scores[2] >= phishing_threshold:
        return 'phishing'
    if scores[1] >= spam_threshold:
        return 'spam'
    return 'legitimate'


def metrics(rows):
    matrix = {label: {p: 0 for p in LABELS + ['error']} for label in LABELS}
    for row in rows:
        if row['label'] not in LABELS:
            raise ValueError('unknown ground truth')
        prediction = row.get('prediction')
        matrix[row['label']][prediction if prediction in LABELS else 'error'] += 1
    counts = {label: sum(matrix[label].values()) for label in LABELS}
    total = sum(counts.values())
    return {'count': total, 'counts': counts, 'confusion': matrix,
            'phishing_recall': matrix['phishing']['phishing'] / counts['phishing'] if counts['phishing'] else None,
            'spam_recall': matrix['spam']['spam'] / counts['spam'] if counts['spam'] else None,
            'legitimate_false_positive_rate': 1 - matrix['legitimate']['legitimate'] / counts['legitimate'] if counts['legitimate'] else None,
            'spam_as_phishing_rate': matrix['spam']['phishing'] / counts['spam'] if counts['spam'] else None,
            'errors': sum(matrix[label]['error'] for label in LABELS),
            'accuracy': sum(matrix[label][label] for label in LABELS) / total if total else None}


def acceptance(measured):
    return (measured['errors'] == 0 and measured['phishing_recall'] is not None and measured['phishing_recall'] >= .95
            and measured['legitimate_false_positive_rate'] is not None and measured['legitimate_false_positive_rate'] <= .02
            and measured['spam_recall'] is not None and measured['spam_recall'] >= .90
            and measured['spam_as_phishing_rate'] is not None and measured['spam_as_phishing_rate'] <= .05)
