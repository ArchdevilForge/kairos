from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from kairos.app import cli


def test_validators(monkeypatch):
    monkeypatch.setattr("kairos.utils.default_symbols.get_prompt", lambda language, key: key)

    assert cli.validate_exchange("okx", "en") == (True, "okx")
    assert cli.validate_exchange("bad", "en") == (False, "invalid_exchange")
    assert cli.validate_timeframe("5m", "en") == (True, "5m")
    assert cli.validate_timeframe("bad", "en") == (False, "invalid_timeframe")
    assert cli.validate_positive_number("1.5", "en") == (True, 1.5)
    assert cli.validate_positive_number("0", "en") == (False, "invalid_threshold")
    assert cli.validate_positive_number("bad", "en") == (False, "invalid_number")
    assert cli.validate_required_chat_id("123", "en") == (True, "123")
    assert cli.validate_required_chat_id("", "en") == (False, "Chat ID is required")
    assert cli.validate_required_chat_id("abc", "en") == (False, "Chat ID must be numeric")


def test_ask_yes_no_empty_uses_default(monkeypatch):
    """Empty input (Enter key) should return the default value per D-04."""
    monkeypatch.setattr("kairos.utils.default_symbols.get_prompt", lambda language, key: key)

    # Default=True branch: blank input resolves to True
    monkeypatch.setattr(cli, "get_user_input", lambda prompt, default=None, secret=False: "")
    assert cli.ask_yes_no("Continue?", "en", default=True) is True

    # Default=False branch: blank input resolves to False
    assert cli.ask_yes_no("Continue?", "en", default=False) is False

    # Affirmative localized input ("是") returns True regardless of default
    monkeypatch.setattr(cli, "get_user_input", lambda prompt, default=None, secret=False: "是")
    assert cli.ask_yes_no("Continue?", "zh", default=False) is True

    # Explicit English affirmative tokens
    monkeypatch.setattr(cli, "get_user_input", lambda prompt, default=None, secret=False: "yes")
    assert cli.ask_yes_no("Continue?", "en", default=False) is True
    monkeypatch.setattr(cli, "get_user_input", lambda prompt, default=None, secret=False: "y")
    assert cli.ask_yes_no("Continue?", "en", default=False) is True


def test_get_validated_input_retries_until_valid(monkeypatch):
    responses = iter(["bad", "okx"])
    monkeypatch.setattr(cli, "get_user_input", lambda prompt, default=None, secret=False: next(responses))

    assert cli.get_validated_input("exchange", "okx", cli.validate_exchange, "en") == "okx"


def test_show_data_info_and_cmd_config_path_print_paths(capsys, monkeypatch, tmp_path):
    monkeypatch.setattr(cli, "get_config_dir", lambda: tmp_path)
    monkeypatch.setattr(cli, "get_config_path", lambda: tmp_path / "config.yaml")
    monkeypatch.setattr(cli, "get_markets_path", lambda: tmp_path / "markets.json")

    cli.show_data_info()
    cli.cmd_config_path(SimpleNamespace())

    output = capsys.readouterr().out
    assert str(tmp_path) in output
    assert "markets.json" in output


def test_update_markets_and_ensure_market_data_paths(tmp_path, monkeypatch):
    monkeypatch.setattr(
        "kairos.utils.supported_markets.refresh_supported_markets", lambda exchanges: {"okx": ["BTC/USDT"]}
    )
    config = {"exchange": "okx"}
    assert cli.update_markets(config) is True

    monkeypatch.setattr("kairos.utils.supported_markets.refresh_supported_markets", lambda exchanges: {})
    assert cli.update_markets(config) is False

    markets_path = tmp_path / "markets.json"
    monkeypatch.setattr(cli, "get_markets_path", lambda: markets_path)
    monkeypatch.setattr(cli, "update_markets", lambda cfg: True)
    assert cli.ensure_market_data(config) is True

    markets_path.write_text('{"okx": ["BTC/USDT"]}', encoding="utf-8")
    assert cli.ensure_market_data(config) is True

    markets_path.write_text('{"binance": []}', encoding="utf-8")
    assert cli.ensure_market_data(config) is True


def test_load_config_and_validate_telegram_token(tmp_path):
    config_path = tmp_path / "config.yaml"
    config_path.write_text("exchange: okx\n", encoding="utf-8")

    assert cli.load_config(config_path)["exchange"] == "okx"
    with pytest.raises(FileNotFoundError):
        cli.load_config(tmp_path / "missing.yaml")

    assert cli._validate_telegram_token({"telegram": {"token": "123:abc"}}) is True
    assert cli._validate_telegram_token({"telegram": {"token": "YOUR_TELEGRAM_TOKEN"}}) is False
    assert cli._validate_telegram_token({"telegram": {"token": "bad"}}) is False


def test_read_pid_file_missing_and_invalid(tmp_path, monkeypatch):
    """_read_pid_file returns None for missing or malformed PID files."""
    pid_path = tmp_path / "kairos.pid"
    monkeypatch.setattr(cli, "get_pid_path", lambda: pid_path)
    assert cli._read_pid_file() is None

    pid_path.write_text("bad", encoding="utf-8")
    assert cli._read_pid_file() is None


def test_get_running_pid_returns_pid_when_running_and_matching(tmp_path, monkeypatch):
    """_get_running_pid returns the PID when both helper checks are positive."""
    pid_path = tmp_path / "kairos.pid"
    monkeypatch.setattr(cli, "get_pid_path", lambda: pid_path)
    pid_path.write_text("123\nkairos.app.runner\n", encoding="utf-8")
    monkeypatch.setattr(cli, "_pid_is_running", lambda pid: True)
    monkeypatch.setattr(cli, "_pid_matches_runner", lambda pid: True)
    assert cli._get_running_pid() == 123


def test_get_running_pid_cleans_stale_pid_file(tmp_path, monkeypatch):
    """_get_running_pid removes the PID file and returns None when runner doesn't match."""
    pid_path = tmp_path / "kairos.pid"
    monkeypatch.setattr(cli, "get_pid_path", lambda: pid_path)
    pid_path.write_text("123\nkairos.app.runner\n", encoding="utf-8")
    monkeypatch.setattr(cli, "_pid_matches_runner", lambda pid: False)
    assert cli._get_running_pid() is None
    assert not pid_path.exists()


def test_pid_is_running_returns_false_on_os_error():
    """Real _pid_is_running returns False when os.kill raises OSError."""
    import os

    original_kill = os.kill
    os.kill = lambda pid, sig: (_ for _ in ()).throw(OSError("dead"))
    try:
        assert cli._pid_is_running(123) is False
    finally:
        os.kill = original_kill


def test_cmd_update_markets_uses_config_and_cached_exchanges(monkeypatch):
    monkeypatch.setattr(cli, "load_config", lambda path: {"exchange": "okx", "exchanges": ["binance"]})
    monkeypatch.setattr(cli, "get_config_path", lambda: Path("/tmp/config.yaml"))
    monkeypatch.setattr(Path, "exists", lambda self: True)
    monkeypatch.setattr("kairos.utils.supported_markets.list_cached_exchanges", lambda: ["bybit"])
    refreshed_args = []
    monkeypatch.setattr(
        "kairos.utils.supported_markets.refresh_supported_markets",
        lambda exchanges: refreshed_args.append(exchanges) or {"okx": []},
    )
    monkeypatch.setattr("kairos.utils.setup_logging.setup_logging", lambda *args, **kwargs: None)

    cli.cmd_update_markets(SimpleNamespace(exchanges=None))

    assert refreshed_args[0] == ["okx", "binance", "bybit"]


def test_run_start_preflight_exits_on_invalid_telegram_or_market_failure(monkeypatch):
    monkeypatch.setattr(cli, "ensure_config_exists", lambda: Path("/tmp/config.yaml"))
    monkeypatch.setattr(cli, "show_data_info", lambda: None)
    monkeypatch.setattr(
        cli,
        "load_config",
        lambda path: {"notificationChannels": ["telegram"], "telegram": {"chatId": ""}},
    )
    monkeypatch.setattr(cli, "_validate_telegram_token", lambda config: False)
    with pytest.raises(SystemExit):
        cli._run_start_preflight()

    monkeypatch.setattr(cli, "_validate_telegram_token", lambda config: True)
    with pytest.raises(SystemExit):
        cli._run_start_preflight()

    monkeypatch.setattr(
        cli,
        "load_config",
        lambda path: {"notificationChannels": [], "exchange": "okx"},
    )
    monkeypatch.setattr(cli, "ensure_market_data", lambda config: False)
    with pytest.raises(SystemExit):
        cli._run_start_preflight()


@pytest.mark.asyncio
async def test_run_monitoring_starts_and_stops_bot_service(monkeypatch):
    sentry = SimpleNamespace(config={"telegram": {"token": "123:abc"}}, run=AsyncMock())
    bot_service = SimpleNamespace(start=AsyncMock(), stop=AsyncMock())
    monkeypatch.setattr("kairos.core.sentry.PriceSentry", lambda: sentry)
    monkeypatch.setattr("kairos.notifications.telegram_bot_service.TelegramBotService", lambda token: bot_service)
    monkeypatch.setattr("kairos.utils.setup_logging.setup_logging", lambda *args, **kwargs: None)

    await cli.run_monitoring()

    bot_service.start.assert_awaited_once()
    sentry.run.assert_awaited_once()
    bot_service.stop.assert_awaited_once()


def test_cmd_run_handles_keyboard_interrupt_and_exception(monkeypatch):
    monkeypatch.setattr("kairos.utils.setup_logging.setup_logging", lambda *args, **kwargs: None)
    monkeypatch.setattr(cli, "_run_start_preflight", lambda: None)

    def raise_keyboard_interrupt(coro):
        coro.close()
        raise KeyboardInterrupt()

    def raise_runtime_error(coro):
        coro.close()
        raise RuntimeError("boom")

    monkeypatch.setattr(cli.asyncio, "run", raise_keyboard_interrupt)
    cli.cmd_run(SimpleNamespace())

    monkeypatch.setattr(cli.asyncio, "run", raise_runtime_error)
    with pytest.raises(SystemExit):
        cli.cmd_run(SimpleNamespace())


def test_cmd_run_invokes_run_monitoring_once(monkeypatch):
    monkeypatch.setattr("kairos.utils.setup_logging.setup_logging", lambda *args, **kwargs: None)
    monkeypatch.setattr(cli, "_run_start_preflight", lambda: None)

    run_calls = []

    def fake_asyncio_run(coro):
        run_calls.append(coro)
        coro.close()

    monkeypatch.setattr(cli.asyncio, "run", fake_asyncio_run)

    cli.cmd_run(SimpleNamespace())

    assert len(run_calls) == 1
