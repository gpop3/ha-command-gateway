"""Config flow de l'intégration Gateway Assist."""

from __future__ import annotations

from typing import Any

import voluptuous as vol

from homeassistant.config_entries import ConfigFlow, ConfigFlowResult

from .const import CONF_API_KEY, CONF_BASE_URL, DOMAIN


class GatewayConfigFlow(ConfigFlow, domain=DOMAIN):
    VERSION = 1

    async def async_step_user(
        self, user_input: dict[str, Any] | None = None
    ) -> ConfigFlowResult:
        errors: dict[str, str] = {}

        if user_input is not None:
            base_url = user_input[CONF_BASE_URL].rstrip("/")
            await self.async_set_unique_id(base_url)
            self._abort_if_unique_id_configured()
            return self.async_create_entry(title="HA Agent", data=user_input)

        schema = vol.Schema(
            {
                vol.Required(
                    CONF_BASE_URL, default="http://192.168.1.50:8080"
                ): str,
                vol.Optional(CONF_API_KEY, default=""): str,
            }
        )
        return self.async_show_form(
            step_id="user", data_schema=schema, errors=errors
        )
