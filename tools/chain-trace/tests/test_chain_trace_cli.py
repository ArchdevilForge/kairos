import importlib.util
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts' / 'chain_trace.py'


class ChainTraceCliTests(unittest.TestCase):
    def test_cli_help_runs(self):
        result = subprocess.run(
            [sys.executable, str(SCRIPT), '--help'],
            check=False,
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 0, msg=result.stderr)
        self.assertIn('Chain Trace', result.stdout)

    def test_solana_support_normalizes_token_and_holder_data(self):
        spec = importlib.util.spec_from_file_location('chain_trace_module', SCRIPT)
        if spec is None or spec.loader is None:
            raise RuntimeError('unable to load scripts/chain_trace.py')

        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)

        tracer = module.ChainTrace(chain='solana', mode='quick')

        class FakeSolscan:
            def token_data(self, mint: str):
                return {
                    'supply': {
                        'value': {
                            'uiAmountString': '975289876.890878',
                            'decimals': 6,
                        }
                    }
                }

            def token_holders(self, mint: str, page: int = 1, page_size: int = 100):
                return {
                    'accounts': [
                        {
                            'address': 'Holder1111111111111111111111111111111111111',
                            'amount': '1200000',
                            'decimals': 6,
                            'uiAmountString': '1.2',
                        },
                        {
                            'address': 'Holder2222222222222222222222222222222222222',
                            'amount': '800000',
                            'decimals': 6,
                            'uiAmountString': '0.8',
                        },
                    ]
                }

        tracer.explorer = FakeSolscan()

        holders = tracer._fetch_holders('mint')

        self.assertEqual(holders[0]['address']['hash'], 'Holder1111111111111111111111111111111111111')
        self.assertAlmostEqual(holders[0]['value'], 1.2)
        self.assertAlmostEqual(holders[1]['value'], 0.8)

    def test_solana_token_info_merges_market_and_mint_metadata(self):
        spec = importlib.util.spec_from_file_location('chain_trace_module', SCRIPT)
        if spec is None or spec.loader is None:
            raise RuntimeError('unable to load scripts/chain_trace.py')

        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)

        tracer = module.ChainTrace(chain='solana', mode='quick')

        class FakeSolscan:
            def account_info(self, mint: str):
                return {
                    'data': {
                        'parsed': {
                            'type': 'mint',
                            'info': {
                                'decimals': 6,
                                'freezeAuthority': None,
                                'mintAuthority': 'Auth1111111111111111111111111111111111111',
                                'supply': '975289876890878',
                            },
                        },
                    },
                }

            def token_data(self, mint: str):
                return {
                    'supply': '975289876.890878',
                    'decimals': 6,
                }

            def token_holders(self, mint: str, page: int = 1, page_size: int = 100):
                return None

        tracer.explorer = FakeSolscan()
        tracer._fetch_solana_market_info = lambda mint: {
            'name': 'MoltID',
            'symbol': 'MOLTID',
            'price_usd': 0.000718,
            'market_cap_usd': 699530.6,
            'fdv_usd': 700280.25,
            'liquidity_usd': 51701.08,
            'volume_24h_usd': 32339.49,
        }

        info = tracer._fetch_token_info('mint')

        self.assertEqual(info['name'], 'MoltID')
        self.assertEqual(info['symbol'], 'MOLTID')
        self.assertAlmostEqual(info['price_usd'], 0.000718)
        self.assertEqual(info['mint_authority'], 'Auth1111111111111111111111111111111111111')
        self.assertIsNone(info['freeze_authority'])

    def test_missing_holder_data_lowers_confidence(self):
        spec = importlib.util.spec_from_file_location('chain_trace_module', SCRIPT)
        if spec is None or spec.loader is None:
            raise RuntimeError('unable to load scripts/chain_trace.py')

        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)

        tracer = module.ChainTrace(chain='solana', mode='deep')
        tracer.results = {
            'token_info': {'name': 'MoltID'},
            'holders': [],
            'suspicious': {'count': 0, 'holders': []},
            'clusters': {'cluster_count': 0, 'anomaly_count': 0, 'risk_score': 0.0},
        }

        risk = tracer._calculate_risk()

        self.assertEqual(risk['verdict'], 'Unknown')
        self.assertLess(risk['confidence_score'], 100)


if __name__ == '__main__':
    unittest.main()
