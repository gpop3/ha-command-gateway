"""Agent conversationnel qui forwarde le texte reconnu vers la gateway."""

from __future__ import annotations

import logging
from typing import Literal

import aiohttp

from homeassistant.components import conversation
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import MATCH_ALL
from homeassistant.core import HomeAssistant
from homeassistant.helpers import intent
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import CONF_API_KEY, CONF_BASE_URL, DEFAULT_TIMEOUT

_LOGGER = logging.getLogger(__name__)


async def async_setup_entry(
        hass: HomeAssistant,
        entry: ConfigEntry,
        async_add_entities: AddEntitiesCallback,
) -> None:
    async_add_entities([GatewayConversationEntity(hass, entry)])


class GatewayConversationEntity(conversation.ConversationEntity):
    """Agent Assist qui délègue toute la compréhension à la gateway.
    """

    _attr_has_entity_name = True
    _attr_name = "Agent Gateway"

    def __init__(self, hass: HomeAssistant, entry: ConfigEntry) -> None:
        self.hass = hass
        self.entry = entry
        self._attr_unique_id = entry.entry_id
        self._base_url = entry.data[CONF_BASE_URL].rstrip("/")
        self._api_key = entry.data.get(CONF_API_KEY, "")

    @property
    def supported_languages(self) -> list[str] | Literal["*"]:
        return MATCH_ALL

    async def async_process(
            self, user_input: conversation.ConversationInput
    ) -> conversation.ConversationResult:
        session = async_get_clientsession(self.hass)

        headers = {"Content-Type": "application/json"}
        if self._api_key:
            headers["Authorization"] = f"Bearer {self._api_key}"

        payload = {
            "text": user_input.text,
            "language": user_input.language,
            "conversation_id": user_input.conversation_id,
        }

        speech = ""
        continue_conversation = False
        try:
            async with session.post(
                    f"{self._base_url}/conversation",
                    json=payload,
                    headers=headers,
                    timeout=aiohttp.ClientTimeout(total=DEFAULT_TIMEOUT),
            ) as resp:
                resp.raise_for_status()
                data = await resp.json()

            speech = data.get("speech") or ""
            continue_conversation = bool(data.get("continue_conversation", False))
            if not data.get("handled", False) and not speech:
                speech = "Je n'ai pas compris la demande."
        except (aiohttp.ClientError, TimeoutError) as err:
            _LOGGER.error("Appel à la gateway échoué : %s", err)
            speech = "La passerelle est injoignable."

        intent_response = intent.IntentResponse(language=user_input.language)
        intent_response.async_set_speech(speech)
        return conversation.ConversationResult(
            response=intent_response,
            conversation_id=user_input.conversation_id,
            continue_conversation=continue_conversation,
        )