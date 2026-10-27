// energy-schema-gauge — обычный гейдж HA (hui-gauge-card: тот же размер,
// центровка, шрифт и цвета порогов, что у соседних), у которого подменена
// только подпись значения. format: days → «1 д 19 ч» / «18 ч 40 м» / «45 м».
// Ставит аддон energy-schema (/local/energy-schema-gauge.js).
(() => {
  if (customElements.get('energy-schema-gauge')) return;
  const two = (n) => String(n).padStart(2, '0');
  const fmtDays = (d) => {
    const m = Math.round(d * 1440);
    const dd = Math.floor(m / 1440), h = Math.floor((m % 1440) / 60), r = m % 60;
    if (dd > 0) return `${dd} д ${two(h)} ч`;
    return h > 0 ? `${h} ч ${two(r)} м` : `${r} м`;
  };
  class EnergySchemaGauge extends HTMLElement {
    setConfig(config) {
      if (!config.entity) throw new Error('entity required');
      this._config = config;
      if (this._inner) this._inner.setConfig(this._gaugeConfig());
    }
    _gaugeConfig() {
      const c = { ...this._config, type: 'gauge' };
      delete c.format;
      return c;
    }
    set hass(hass) {
      this._hass = hass;
      this._update();
    }
    async _update() {
      if (!this._inner) {
        this._creating = this._creating || (async () => {
          const helpers = await window.loadCardHelpers();
          this._inner = helpers.createCardElement(this._gaugeConfig());
          this.appendChild(this._inner);
        })();
        await this._creating;
      }
      this._inner.hass = this._hass;
      await this._inner.updateComplete;
      const g = this._inner.shadowRoot && this._inner.shadowRoot.querySelector('ha-gauge');
      const st = this._hass.states[this._config.entity];
      const v = st ? parseFloat(st.state) : NaN;
      if (!g || isNaN(v)) return;
      if (this._config.format === 'days') g.valueText = fmtDays(v);
      g.label = '';
    }
    getCardSize() {
      return 4;
    }
    getGridOptions() {
      return { columns: 6, rows: 'auto', min_columns: 4 };
    }
  }
  customElements.define('energy-schema-gauge', EnergySchemaGauge);
  window.customCards = window.customCards || [];
  window.customCards.push({ type: 'energy-schema-gauge', name: 'Energy schema gauge', description: 'HA gauge with a formatted value (days → «1 д 19 ч»)' });
})();
