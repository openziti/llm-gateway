import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
    docsSidebar: [
        {type: 'html', value: '<span class="menu__link">INTRO</span>', className: 'sidebar-title'},
        {type: 'doc', id: 'intro'},
        {type: 'doc', id: 'get-started'},
        {type: 'html', value: '<span class="menu__link">HOW-TO</span>', className: 'sidebar-title'},
        {type: 'doc', id: 'connect-zrok'},
        {type: 'html', value: '<span class="menu__link">LEARN</span>', className: 'sidebar-title'},
        {type: 'doc', id: 'semantic-routing'},
        {type: 'doc', id: 'multi-endpoint'},
        {type: 'html', value: '<span class="menu__link">REFERENCE</span>', className: 'sidebar-title'},
        {type: 'doc', id: 'configuration'},
        {type: 'doc', id: 'providers'},
        {type: 'doc', id: 'api-keys'},
        {type: 'doc', id: 'streaming'},
        {type: 'doc', id: 'metrics'},
    ],
};

export default sidebars;
