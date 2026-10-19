import unittest
from types import SimpleNamespace
import mlx.core as mx
from decision import classification_loss, class_scores
from run import validate_objective


class Body:
    def __init__(self, values):
        self.values = values
        self.embed_tokens = SimpleNamespace(as_linear=lambda x: x)
        self.last_inputs = None

    def __call__(self, inputs):
        self.last_inputs = inputs
        return self.values


def model(values):
    return SimpleNamespace(model=Body(values), args=SimpleNamespace(tie_word_embeddings=True))


class DecisionTests(unittest.TestCase):
    def test_objective_cannot_silently_revert_to_token_formatting_loss(self):
        manifest = {'objective': 'three-way-class-cross-entropy-at-prompt-end-v2'}
        validate_objective(manifest, 'class-decision')
        validate_objective({}, 'json-token')
        with self.assertRaises(ValueError):
            validate_objective(manifest, 'json-token')
        with self.assertRaises(ValueError):
            validate_objective({}, 'class-decision')

    def test_only_class_decision_has_gradient(self):
        batch = mx.array([[1, 2, 3, 4, 32, 9, 0], [1, 2, 3, 4, 34, 9, 0]])
        lengths = mx.array([[4, 6], [4, 6]])
        def loss(values):
            return classification_loss(model(values), batch, lengths)[0]
        values = mx.zeros((2, 7, 40))
        gradient = mx.grad(loss)(values)
        mx.eval(gradient)
        expected = mx.zeros_like(gradient)
        expected[0, 3, 32:35] = mx.array([-1 / 3, 1 / 6, 1 / 6])
        expected[1, 3, 32:35] = mx.array([1 / 6, 1 / 6, -1 / 3])
        self.assertTrue(mx.allclose(gradient, expected, atol=1e-6).item())
        measured, decisions = classification_loss(model(values), batch, lengths)
        self.assertAlmostEqual(measured.item(), 1.0986123, places=5)
        self.assertEqual(decisions.item(), 2)

    def test_gold_answer_and_formatting_are_removed_from_forward_input(self):
        values = mx.zeros((1, 7, 40))
        actual = model(values)
        first = class_scores(actual, mx.array([[1, 2, 3, 4, 32, 9, 99]]), mx.array([4]))
        self.assertEqual(actual.model.last_inputs.tolist(), [[1, 2, 3, 4, 0, 0, 0]])
        second = class_scores(actual, mx.array([[1, 2, 3, 4, 34, 88, 77]]), mx.array([4]))
        self.assertTrue(mx.array_equal(first, second).item())
        self.assertEqual(actual.model.last_inputs.tolist(), [[1, 2, 3, 4, 0, 0, 0]])


if __name__ == '__main__':
    unittest.main()
