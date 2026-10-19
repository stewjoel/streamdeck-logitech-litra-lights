/// <reference path="../libs/js/property-inspector.js" />
/// <reference path="../libs/js/action.js" />
/// <reference path="../libs/js/utils.js" />

$PI.onConnected((jsn) => {
    const { actionInfo, appInfo, connection, messageType, port, uuid } = jsn;
    const { payload, context } = actionInfo;
    const { settings } = payload;

    if (actionInfo && actionInfo.action) {
        const section = document.getElementById(actionInfo.action)
        if (section) {
            section.style.display = "block"
            const form = section.querySelector('form');
            if (form) {
                Utils.setFormValue(settings, form);
                form.addEventListener(
                    'input',
                    Utils.debounce(150, () => {
                        const value = Utils.getFormValue(form);
                        $PI.setSettings(value);
                    })
                );
            }
            // Back Gradient Cycle: load gradient presets
            if (actionInfo.action === 'ca.michaelabon.logitech-litra-lights.back.presets') {
                if (settings.presets && typeof updatePresetsUI === 'function') {
                    updatePresetsUI(settings.presets);
                }
            }
            // Back Color Cycle: load color presets
            if (actionInfo.action === 'ca.michaelabon.logitech-litra-lights.back.color') {
                if (typeof updateColorPresetsUI === 'function') {
                    updateColorPresetsUI(settings.colorPresets);
                }
            }
        }
    }

    window.onGetSettingsClick = (url) => {
        $PI.send(this.UUID, "openUrl", { payload: { url } })
    }
});

$PI.onDidReceiveGlobalSettings(({ payload }) => {
    console.log('onDidReceiveGlobalSettings', payload);
})

/**
 * Provide window level functions to use in the external window
 * (this can be removed if the external window is not used)
 */
window.sendToInspector = (data) => {
    console.log(data);
};
