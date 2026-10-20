const { Resvg } = require('@resvg/resvg-js');
const fs = require('fs');
const path = require('path');

const iconsDir = path.resolve(__dirname, '../com.stewjoel.logitech-litra-lights.sdPlugin/icons');

console.log('Rendering all icons in:', iconsDir);

const files = fs.readdirSync(iconsDir).filter(f => f.endsWith('.svg'));

for (const file of files) {
    const baseName = path.basename(file, '.svg');
    const svgPath = path.join(iconsDir, file);
    const svgData = fs.readFileSync(svgPath);

    if (baseName === 'pluginIcon') {
        // Plugin Hero Icon (72x72, 144x144, 256x256)
        const resvg1 = new Resvg(svgData, { fitTo: { mode: 'width', value: 72 } });
        fs.writeFileSync(path.join(iconsDir, 'pluginIcon.png'), resvg1.render().asPng());

        const resvg2 = new Resvg(svgData, { fitTo: { mode: 'width', value: 144 } });
        fs.writeFileSync(path.join(iconsDir, 'pluginIcon@2x.png'), resvg2.render().asPng());
        console.log(`Rendered: pluginIcon.png (72x72) and pluginIcon@2x.png (144x144)`);
    } else if (baseName === 'category_icon') {
        // Category Icon (28x28 and 56x56)
        const resvg1 = new Resvg(svgData, { fitTo: { mode: 'width', value: 28 } });
        fs.writeFileSync(path.join(iconsDir, 'category_icon.png'), resvg1.render().asPng());

        const resvg2 = new Resvg(svgData, { fitTo: { mode: 'width', value: 56 } });
        fs.writeFileSync(path.join(iconsDir, 'category_icon@2x.png'), resvg2.render().asPng());
        console.log(`Rendered: category_icon.png (28x28) and category_icon@2x.png (56x56)`);
    } else {
        // Action Icons (72x72 standard, 144x144 retina)
        const resvg1 = new Resvg(svgData, { fitTo: { mode: 'width', value: 72 } });
        fs.writeFileSync(path.join(iconsDir, `${baseName}.png`), resvg1.render().asPng());

        const resvg2 = new Resvg(svgData, { fitTo: { mode: 'width', value: 144 } });
        fs.writeFileSync(path.join(iconsDir, `${baseName}@2x.png`), resvg2.render().asPng());
        console.log(`Rendered: ${baseName}.png (72x72) and ${baseName}@2x.png (144x144)`);
    }
}

console.log('All icons successfully rendered to PNG and @2x.png!');
