/// <reference path="../libs/js/property-inspector.js" />
/// <reference path="../libs/js/action.js" />
/// <reference path="../libs/js/utils.js" />

$PI.onConnected((jsn) => {
    const { actionInfo } = jsn;
    const { payload } = actionInfo;
    const { settings } = payload;

    window.currentActionSettings = Object.assign({}, settings || {});

    if (typeof renderMultiDeviceUI === 'function') {
        renderMultiDeviceUI(window.currentActionSettings);
    }

    if (actionInfo && actionInfo.action) {
        // Hide target selector on the Sync Mode toggle key itself (it manages the pair group directly)
        const targetRow = document.getElementById('target-selector-row');
        if (targetRow) {
            targetRow.style.display = (actionInfo.action === 'com.stewjoel.logitech-litra-lights.sync') ? 'none' : 'flex';
        }

        const section = document.getElementById(actionInfo.action);
        if (section) {
            section.style.display = "block";
            const form = section.querySelector('form');
            if (form) {
                Utils.setFormValue(settings, form);
                form.addEventListener(
                    'input',
                    Utils.debounce(150, () => {
                        const value = Utils.getFormValue(form);
                        saveMergedSettings(value);
                    })
                );
            }
            if (actionInfo.action === 'com.stewjoel.logitech-litra-lights.back.presets') {
                if (typeof updatePresetsUI === 'function') {
                    updatePresetsUI(settings.presets);
                }
            }
            if (actionInfo.action === 'com.stewjoel.logitech-litra-lights.back.color') {
                if (typeof updateColorPresetsUI === 'function') {
                    updateColorPresetsUI(settings.colorPresets);
                }
            }
        }
    }
});

$PI.onDidReceiveGlobalSettings(({ payload }) => {
    console.log('onDidReceiveGlobalSettings', payload);
});

window.sendToInspector = (data) => {
    console.log(data);
};
