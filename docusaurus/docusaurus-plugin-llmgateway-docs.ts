import * as path from 'path';
import type { PluginConfig } from '@docusaurus/types';
import { LogLevel, remarkScopedPath, remarkCodeSections } from "@netfoundry/docusaurus-theme/plugins";
import remarkGithubAdmonitionsToDirectives from "remark-github-admonitions-to-directives";

export function llmgatewayDocsPluginConfig(
    rootDir: string,
    linkMappings: { from: string; to: string }[],
    routeBasePath: string = 'docs/llm-gateway'
): PluginConfig {
    return [
        '@docusaurus/plugin-content-docs',
        {
            id: 'llm-gateway',
            path: path.resolve(rootDir, 'docs'),
            routeBasePath,
            sidebarPath: path.resolve(rootDir, 'sidebars.ts'),
            includeCurrentVersion: true,
            beforeDefaultRemarkPlugins: [
                remarkGithubAdmonitionsToDirectives,
            ],
            remarkPlugins: [
                [remarkScopedPath, { mappings: linkMappings, logLevel: LogLevel.Silent }],
                [remarkCodeSections, { logLevel: LogLevel.Silent }],
            ],
        } as any,
    ];
}
